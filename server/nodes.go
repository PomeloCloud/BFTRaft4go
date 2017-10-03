package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"strconv"
	"github.com/patrickmn/go-cache"
)

type NodeIterator struct {
	prefix []byte
	data *badger.Iterator
	server *BFTRaftServer
}

func (liter *NodeIterator) Next() *pb.Node  {
	liter.data.Next()
	if liter.data.ValidForPrefix(liter.prefix) {
		nodeId := BytesU64(liter.data.Item().Key(), len(liter.prefix))
		return liter.server.GetNode(nodeId)
	} else {
		return nil
	}
}

func (s *BFTRaftServer) NodesIterator () NodeIterator {
	keyPrefix := ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST)
	iter := s.DB.NewIterator(badger.IteratorOptions{PrefetchValues: false})
	iter.Seek(append(keyPrefix, U64Bytes(0)...))
	return NodeIterator{
		prefix: keyPrefix,
		data: iter,
		server: s,
	}
}

func (s *BFTRaftServer) GetNode (nodeId uint64) *pb.Node {
	cacheKey := strconv.Itoa(int(nodeId))
	if cacheNode, cachedFound := s.Nodes.Get(cacheKey); cachedFound {
		return cacheNode.(*pb.Node)
	}
	item :=  badger.KVItem{}
	s.DB.Get(append(ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST), U64Bytes(nodeId)...), &item)
	data := ItemValue(&item)
	if data == nil {
		return nil
	}
	node := pb.Node{}
	proto.Unmarshal(*data, &node)
	s.Nodes.Set(cacheKey, &node, cache.DefaultExpiration)
	return &node
}
