package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
)

type LogEntryIterator struct {
	prefix []byte
	data *badger.Iterator
}

func (liter *LogEntryIterator) Next() *pb.LogEntry  {
	liter.data.Next()
	if liter.data.ValidForPrefix(liter.prefix) {
		entry := pb.LogEntry{}
		itemData := ItemValue(liter.data.Item())
		proto.Unmarshal(itemData, &entry)
		return &entry
	} else {
		return nil
	}
}

func (s *BFTRaftServer) ReversedLogIterator (group uint64) LogEntryIterator {
	keyPrefix := ComposeKeyPrefix(group, RAFT_LOGS)
	iter := s.DB.NewIterator(badger.IteratorOptions{Reverse: true})
	iter.Seek(append(keyPrefix, U64Bytes(^uint64(0))...)) // search from max possible index
	return LogEntryIterator{
		prefix: keyPrefix,
		data: iter,
	}
}
