package server

import (
	"encoding/binary"
	"github.com/dgraph-io/badger"
)

const (
	RAFT_LOGS = 0
	GROUP_PEERS = 1
	NODES_LIST = 3
)

const (
	NODE_LIST_GROUP = 1
)

func U32Bytes(t uint32) []byte {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, t)
	return bs
}

func U64Bytes(n uint64) []byte {
	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, n)
	return bs
}

func ComposeKeyPrefix (group uint64, t uint32) []byte {
	return append(U64Bytes(group), U32Bytes(t)...)
}

func ItemValue(item *badger.KVItem) []byte {
	var val []byte
	item.Value(func(bytes []byte) error {
		val = make([]byte, len(bytes))
		copy(val, bytes)
		return nil
	})
	return val
}
