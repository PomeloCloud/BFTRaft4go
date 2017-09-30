package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
)

type NodeIterator struct {
	prefix []byte
	data *badger.Iterator
}

func (liter *NodeIterator) Next() *pb.Node  {
	liter.data.Next()
	if liter.data.ValidForPrefix(liter.prefix) {
		entry := pb.Node{}
		itemData := ItemValue(liter.data.Item())
		proto.Unmarshal(itemData, &entry)
		return &entry
	} else {
		return nil
	}
}