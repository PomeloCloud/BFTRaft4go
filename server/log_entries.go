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