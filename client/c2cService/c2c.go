package c2cService

import (
	"blabu/c2cService/client"
	cf "blabu/c2cService/configuration"
	c2cData "blabu/c2cService/data/c2cdata"
	"blabu/c2cService/dto"
	log "blabu/c2cService/logWrapper"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
)

var connection client.ConnectionCache

func init() {
	connection = client.NewConnectionCache()
}

//C2cError Ошибка клиентской логики
type C2cError struct {
	ErrType uint16
	text    string
}

// Возможные типы ошибок клиентской логики
const (
	ClientNotFindError uint16 = iota + 1
	ReadTimeoutError
	ClientExcistError
	UnsupportedCommandError
	/*=================================================================================================================*/
	DisableConnectionErrorLimit // Все ошибки ниже системные и отправлять подзапрос на другие сервера не имеет смысла
	InternalError
	BadCommandError
	InvalidCredentials
	BadMessageError
	NilMessageError
)

// Error - реализация интерфейса ошибки для c2c устройств
func (err C2cError) Error() string {
	return err.text
}

// NewC2cError Создание новой ошибки
func NewC2cError(t uint16, text string) error {
	return C2cError{
		t,
		text,
	}
}

// Errorf Создание новой ошибки из форматированной строки
func Errorf(t uint16, format string, data ...interface{}) error {
	return C2cError{
		ErrType: t,
		text:    fmt.Sprintf(format, data...),
	}
}

// C2cDevice - Сущность реализующая интерфейс клиента для двустороннего обмена сообщениями
// и интерфейс ClientListenerInterface для добавления его в кеш
type C2cDevice struct {
	sessionID    uint32
	clientType   c2cData.ClientType
	storage      c2cData.DB
	device       dto.ClientDescriptor // Номер устройства
	readChan     chan dto.Message
	listenerList map[uint64]*chan dto.Message // Список каналов устройств слушающих отправляемые сообщения этого клиента
	listenerMtx  sync.RWMutex                 // Для защиты списка каналов устройств слушающих сообщения этого клиента
}

// AddListener - Добавляет нового слушателя в список подписчиков для раздачи данных
func (c *C2cDevice) AddListener(from uint64, ch *chan dto.Message) {
	if ch != nil {
		c.listenerMtx.Lock()
		c.listenerList[from] = ch
		c.listenerMtx.Unlock()
		log.Tracef("Add channel from client %x to %x name %s", from, c.device.ID, c.device.Name)
	}
}

// DelListener - Удаляем слушателя из списка подписчиков для конкретного клиента
func (c *C2cDevice) DelListener(from uint64) {
	c.listenerMtx.Lock()
	delete(c.listenerList, from)
	c.listenerMtx.Unlock()
	log.Tracef("Delete channel from client %x for %s", from, c.device.Name)
}

// GetListenerChan - Необходим для подключения одного клиента к другому в кеше клиентов
func (c *C2cDevice) GetListenerChan() *chan dto.Message {
	return &c.readChan
}

// NewC2cDevice - Конструктор нового клеинта
func NewC2cDevice(db c2cData.DB, sessionID uint32, maxConnection uint32) client.ReadWriteCloser {
	clTypeStr := cf.GetConfigValueOrDefault("ClientType", "0")
	clType, _ := strconv.ParseUint(clTypeStr, 10, 16)
	if clType == 0 {
		log.Error("Clinet type for this server does not specified. Registartion is disabled")
	}
	var c = new(C2cDevice)
	c.sessionID = sessionID
	c.storage = db
	c.readChan = make(chan dto.Message, maxConnection) // Делаем его буферизированным, чтобы много узлов смогли отпраить ему сообщение
	c.listenerList = make(map[uint64]*chan dto.Message)
	c.clientType = c2cData.ClientType(clType)
	return c
}

// Write - обработка сообщений в соответствии с командами
func (c *C2cDevice) Write(msg *dto.Message) error {
	if msg == nil {
		return Errorf(NilMessageError, "Message is nil in session %d", c.sessionID)
	}
	switch msg.Command {
	case client.ErrorCOMMAND:
		return c.errorHandler(msg)
	case client.PingCOMMAND:
		return c.ping(msg)
	case client.ConnectByIDCOMMAND: // Content[0] - from ID, Content[1] - to ID
		return c.connectByID(msg)
	case client.ConnectByNameCOMMAND: // Content[0] - from name, Content[1] - to name
		return c.connectByName(msg)
	case client.InitByIDCOMMAND: // Content[0] - from ID, Content[1] - to (server always "0")
		return c.initByID(msg)
	case client.InitByNameCOMMAND: // Content[0] - from name, Content[1] - to (server always "0")
		return c.initByName(msg)
	case client.RegisterCOMMAND:
		if c.clientType != 0 {
			return c.registerNewDevice(msg) // Content[0] - from name, Content[1] - to (server always "0") , Content[2] - BASE64(SHA256(name+password))
		}
		return NewC2cError(UnsupportedCommandError, "Registartion is disabled for this server")
	case client.GenerateCOMMAND:
		if c.clientType != 0 {
			return c.generateNewDevice(msg) // Content[0] - is empty, Content[1] - to (server always "0"), Content[2] - BASE64 string password hash
		}
		return NewC2cError(UnsupportedCommandError, "Generate new device is disabled for this server")
	case client.DataCOMMAND:
		return c.sendNewMessage(msg)
	case client.DestroyConCOMMAND: // Разорвать соединения без отключения от сервера
		return c.destroyConnection(msg) //Content[0] - from: local ID or Name, Content[1] - destroy connection from who.
	case client.PropertiesCOMMAND:
		return c.setProperies(msg) //Content[0] - from: local ID or Name, Content[1] - to
	default:
		return Errorf(UnsupportedCommandError, "Unsupported command %d in session %d", msg.Command, c.sessionID)
	}
}

//Read - читаем ответ от бизнес логики или стороннего клиента
//Ждущая функция, вернет управления если:
// 1. Приготовлен ответ
// 2. Истекло время ожидания ответа
// 3. Произшла ошибка чтения
func (c *C2cDevice) Read(dt time.Duration, handler func(msg dto.Message, err error) error) {
	t := time.NewTimer(dt)
	for {
		select {
		case m, ok := <-c.readChan:
			if !ok {
				t.Stop()
				log.Tracef("Read channel is closed for device %x name %s for session %d", c.device.ID, c.device.Name, c.sessionID)
				handler(dto.Message{}, io.EOF)
				return
			}
			handler(m, nil)
		case <-t.C:
			err := Errorf(ReadTimeoutError, "Read timeout in session %d Read is continue", c.sessionID)
			handler(dto.Message{}, err)
			t.Reset(dt)
		}
	}
}

// Close - информирует бизнес логику про разрыв соединения
func (c *C2cDevice) Close() error {
	c.destroyConnection(&dto.Message{
		From:    c.device.Name,
		To:      "0",
		Command: client.DestroyConCOMMAND,
		Jmp:     1, // TODO set Jmp obviously is a bad practice
		Proto:   1, // TODO set Proto obviously is a bad practice
	})
	connection.DelClientFromCashe(c.device.ID)
	close(c.readChan)
	log.Infof("Close client %s with id %d in session %d", c.device.Name, c.device.ID, c.sessionID)
	c.device.ID = 0
	return nil
}

// GetID - возвращет идентификатор текущего клиента
func (c *C2cDevice) GetID() uint64 {
	return c.device.ID
}
