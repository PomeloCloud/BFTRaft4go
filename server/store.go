package server

import (
	"github.com/dgraph-io/badger"
	"log"
	"github.com/PomeloCloud/BFTRaft4go/utils"
)

const (
	LOG_ENTRIES    = 0
	GROUP_PEERS    = 1
	GROUP_META     = 2
	HOST_LIST      = 3
	GROUP_LAST_IDX = 4
	SERVER_CONF    = 100
)

const (
	HOST_LIST_GROUP = 1
	CONFIG_GROUP    = 0
)

func ComposeKeyPrefix(group uint64, t uint32) []byte {
	return append(utils.U32Bytes(t), utils.U64Bytes(group)...)
}

func ItemValue(item *badger.Item) *[]byte {
	if item == nil {
		return nil
	}
	if val, err := item.Value(); err == nil {
		return &val
	} else {
		log.Println("cannot get value:", err)
		return nil
	}
}
