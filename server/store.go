package server

import (
	"encoding/binary"
	"github.com/dgraph-io/badger"
)

const (
	RAFT_LOGS = 0
	GROUP_PEERS = 1
	LAST_KEY = 2
)

func U32Bytes(t uint32) []byte {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, t)
	return bs
}

func ComposeKeyPrefix (group int64, t uint32) []byte {
	groupBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(groupBytes, uint64(group))
	return append(groupBytes, U32Bytes(t)...)
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

func (s *BFTRaftServer) LastRaftLogKey(group int64) ([]byte, error)  {
	var item badger.KVItem
	if err := s.DB.Get(ComposeKeyPrefix(group, LAST_KEY), &item); err != nil {
		return nil, err
	}
	return ItemValue(&item), nil
}
