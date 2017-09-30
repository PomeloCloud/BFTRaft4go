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

func (s *BFTRaftServer) NodesIterator () NodeIterator {
	keyPrefix := ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST)
	iter := s.DB.NewIterator(badger.IteratorOptions{})
	iter.Seek(append(keyPrefix, U64Bytes(0)...))
	return NodeIterator{
		prefix: keyPrefix,
		data: iter,
	}
}

func (s *BFTRaftServer) OnlineNodes() []*pb.Node  {
	iter := s.NodesIterator()
	nodes := []*pb.Node{}
	for true {
		if node := iter.Next(); node != nil {
			if node.Online {
				nodes = append(nodes, node)
			}
		} else {
			break
		}
	}
	return nodes
}