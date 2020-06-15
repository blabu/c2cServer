package clientFactory

import (
	"blabu/c2cService/client"
	"blabu/c2cService/client/c2cService"
	"blabu/c2cService/client/savemsgservice"
	"blabu/c2cService/client/trafficclient"
	conf "blabu/c2cService/configuration"
	"blabu/c2cService/data/c2cdata"
	"blabu/c2cService/parser"
	"strconv"
	"strings"
)

//CreateClientLogic - create client for c2c and add some middleware
func CreateClientLogic(p parser.Parser, sessionID uint32) client.ReadWriteCloser {
	m, e := strconv.ParseUint(conf.GetConfigValueOrDefault("MaxQueuePacketSize", "64"), 10, 32)
	if e != nil {
		m = 64
	}
	switch p.GetParserType() {
	case parser.C2cParserType:
		db := c2cdata.GetBoltDbInstance()
		client := c2cService.NewC2cDevice(db, sessionID, uint32(m))
		list := conf.GetConfigValueOrDefault("MiddlewareClientList", "")
		middle := strings.Split(list, ",")
		for _, v := range middle {
			switch v {
			case "safe":
				client = savemsgservice.NewDecorator(db, client)
			case "limits":
				client = trafficclient.GetNewTraficCounterWrapper(db, client)
			}
		}
		return client
	default:
		return nil
	}
}
