package server

import (
	"encoding/binary"
	"github.com/dgraph-io/badger"
)

const (
	LOG_ENTRIES   = 0
	GROUP_PEERS   = 1
	GROUP_META    = 2
	NODES_LIST    = 3
	GROUP_MAX_IDX = 4
	SERVER_CONF   = 100
)

const (
	NODE_LIST_GROUP = 1
	CONFIG_GROUP    = 0
)

const (
	UINT32_LEN = 4
	UINT64_LEN = 8
)

func U32Bytes(t uint32) []byte {
	bs := make([]byte, UINT32_LEN)
	binary.BigEndian.PutUint32(bs, t)
	return bs
}

func U64Bytes(n uint64) []byte {
	bs := make([]byte, UINT64_LEN)
	binary.BigEndian.PutUint64(bs, n)
	return bs
}

func BytesU64(bs []byte, offset int) uint64 {
	return binary.BigEndian.Uint64(bs[offset : offset+UINT64_LEN])
}

func ComposeKeyPrefix(group uint64, t uint32) []byte {
	return append(U32Bytes(t), U64Bytes(group)...)
}

func ItemValue(item *badger.KVItem) *[]byte {
	var val *[]byte
	item.Value(func(bytes []byte) error {
		if bytes != nil {
			lval := make([]byte, len(bytes))
			copy(lval, bytes)
			val = &lval
		} else {
			val = nil
		}
		return nil
	})
	return val
}
