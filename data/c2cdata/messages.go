package c2cdata

import (
	"blabu/c2cService/dto"
	"encoding/json"
	"errors"
	"fmt"

	bolt "github.com/etcd-io/bbolt"
)

type IMessage interface {
	IsSended(userID uint64, messageID uint64)
	Add(userID uint64, msg dto.UnSendedMsg) (uint64, error)
	GetNext(userID uint64) (dto.UnSendedMsg, error)
}

type Messages struct {
	messageStorage *bolt.DB
}

func (m *Messages) IsSended(userID uint64, messageID uint64) {
	update(uint64ToBytes(userID), m.messageStorage, func(buck *bolt.Bucket) error {
		return buck.Delete(uint64ToBytes(messageID))
	})
}

func (m *Messages) Add(userID uint64, msg dto.UnSendedMsg) (uint64, error) {
	var messageID uint64
	err := update(uint64ToBytes(userID), m.messageStorage, func(buck *bolt.Bucket) error {
		msg.ID, _ = buck.NextSequence()
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		messageID = msg.ID
		return buck.Put(uint64ToBytes(msg.ID), data)
	})
	if err != nil {
		return 0, fmt.Errorf("Can not add message from %s to %d", msg.From, userID)
	}
	return messageID, nil
}

func (m *Messages) GetNext(userID uint64) (dto.UnSendedMsg, error) {
	var msg dto.UnSendedMsg
	err := view(uint64ToBytes(userID), m.messageStorage, func(buck *bolt.Bucket) error {
		key, value := buck.Cursor().First()
		if key == nil || value == nil {
			return errors.New("Empty message list")
		}
		err := json.Unmarshal(value, &msg)
		if err != nil {
			return err
		}
		msg.ID = bytesToUint64(key)
		return nil
	})
	return msg, err
}
