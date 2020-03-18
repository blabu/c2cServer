package trafficClient

import (
	"blabu/c2cService/client"
	"blabu/c2cService/data/c2cData"
	"blabu/c2cService/dto"
	log "blabu/c2cService/logWrapper"
	"io"
	"time"
)

type traficCounterWrapper struct {
	storage  c2cData.DB
	client   client.ReadWriteCloser
	stat     dto.ClientStat
	validate func(st *dto.ClientStat) error
}

//GetNewTraficCounterWrapper - вернет обертку, которая реализует подсчет трафика принятых и отправленных байт
func GetNewTraficCounterWrapper(storage c2cData.DB, cl client.ReadWriteCloser) client.ReadWriteCloser {
	return &traficCounterWrapper{
		storage:  storage,
		client:   cl,
		stat:     dto.ClientStat{},
		validate: updateLimits,
	}
}

func (c *traficCounterWrapper) Write(msg *dto.Message) error {
	if c.stat.ID == 0 {
		log.Tracef("Try init clinet %s stat in write method", msg.From)
		rc := c.stat.ReceiveBytes
		tr := c.stat.TransmiteBytes
		c.stat, _ = initStat(msg.From, c.storage)
		c.stat.TransmiteBytes += tr
		c.stat.ReceiveBytes += rc
	} else if er := c.validate(&c.stat); er != nil {
		log.Error(er.Error())
		return er
	}
	c.stat.ReceiveBytes += uint64(len(msg.Content))
	return c.client.Write(msg)
}

func (c *traficCounterWrapper) Read(dt time.Duration, handler func(msg dto.Message, err error)) {
	c.client.Read(dt, func(msg dto.Message, err error) {
		if c.stat.ID == 0 {
			log.Tracef("Try init clinet %s stat in read method", msg.From)
			rc := c.stat.ReceiveBytes
			tr := c.stat.TransmiteBytes
			c.stat, _ = initStat(msg.From, c.storage)
			c.stat.TransmiteBytes += tr
			c.stat.ReceiveBytes += rc
		} else if err == nil {
			if er := c.validate(&c.stat); er != nil {
				log.Error(er.Error())
				handler(dto.Message{}, io.EOF)
				return
			}
		}
		handler(msg, err)
		c.stat.TransmiteBytes += uint64(len(msg.Content))
	})
}

func (c *traficCounterWrapper) Close() error {
	if err := c.storage.UpdateStat(&c.stat); err != nil {
		log.Error(err.Error())
	} else {
		log.Tracef("Save stat fine %v", c.stat)
	}
	return c.client.Close()
}
