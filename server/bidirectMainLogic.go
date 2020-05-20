package server

import (
	"blabu/c2cService/client"
	"blabu/c2cService/clientFactory"
	"blabu/c2cService/dto"
	log "blabu/c2cService/logWrapper"
	"blabu/c2cService/parser"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// bidirectMainLogic - двунаправленная реализация MainLogicIO для независимого чтения и записи информации
// Реализовано:
// 1. чтение с клиента и запись в сеть метод Read()
// 2. запись в клиента метод Write()
type bidirectMainLogic struct {
	sessionID uint32
	dt        time.Duration
	p         parser.Parser
	c         client.ReadWriteCloser
}

//CreateReadWriteMainLogic - Создаем новый интерфейс для MainLogicIO (логики взаимодействия сервера и клиентской логики)
//!!!НИКОГДА НЕ ВОЗРАЩАЕТ NIL!!!
func CreateReadWriteMainLogic(p parser.Parser, readTimeout time.Duration) MainLogicIO {
	sesID := atomic.AddUint32(&lastSessionID, 1)
	return &bidirectMainLogic{
		sessionID: sesID,
		dt:        readTimeout,
		p:         p,
		c:         clientFactory.CreateClientLogic(p, sesID),
	}
}

// Write - синхронный вызов парсит сообщение полученное с сети и пишет данные в клиентскую логику
func (s *bidirectMainLogic) Write(data []byte) (int, error) {
	if s.c == nil {
		return 0, errors.New("Nil error")
	}
	m, err := s.p.ParseMessage(data)
	if err != nil {
		log.Warningf("Can not parse message in session %d. Error %s", s.sessionID, err.Error())
		return 0, err
	}
	return len(data), s.c.Write(&m)
}

//Read - читает из бизнес логики и передает данные обработчику handler
func (s *bidirectMainLogic) Read(handler func([]byte, error) error) {
	if s.c == nil {
		handler(nil, errors.New("Parser or client is nil"))
		return
	}
	s.c.Read(s.dt, func(msg dto.Message, err error) error {
		if err != nil {
			if err == io.EOF { // Читать больше нечего
				log.Info(err.Error())
				handler(nil, io.EOF)
			}
			return nil
		}
		log.Trace("Received data from client logic fine")
		return handler(s.p.FormMessage(msg))
	})
}

// Close - закрываем соединения с клиентской логикой
func (s *bidirectMainLogic) Close() error {
	log.Infof("Close bidirectMainLogic and client logic in session %d", s.sessionID)
	if s.c != nil {
		s.c.Close()
		return nil
	}
	return fmt.Errorf("Client logic is nil")
}
