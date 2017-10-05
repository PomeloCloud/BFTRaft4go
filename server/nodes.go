package server

import (
	"crypto/rsa"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
)

type NodeIterator struct {
	prefix []byte
	data   *badger.Iterator
	server *BFTRaftServer
}

func (liter *NodeIterator) Next() *pb.Node {
	liter.data.Next()
	if liter.data.ValidForPrefix(liter.prefix) {
		nodeId := BytesU64(liter.data.Item().Key(), len(liter.prefix))
		return liter.server.GetNode(nodeId)
	} else {
		return nil
	}
}

func (s *BFTRaftServer) NodesIterator() NodeIterator {
	keyPrefix := ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST)
	iter := s.DB.NewIterator(badger.IteratorOptions{PrefetchValues: false})
	iter.Seek(append(keyPrefix, U64Bytes(0)...))
	return NodeIterator{
		prefix: keyPrefix,
		data:   iter,
		server: s,
	}
}

func (s *BFTRaftServer) GetNode(nodeId uint64) *pb.Node {
	cacheKey := strconv.Itoa(int(nodeId))
	if cacheNode, cachedFound := s.Nodes.Get(cacheKey); cachedFound {
		return cacheNode.(*pb.Node)
	}
	item := badger.KVItem{}
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

func (s *BFTRaftServer) GetNodePublicKey(nodeId uint64) *rsa.PublicKey {
	cacheKey := strconv.Itoa(int(nodeId))
	if cachedKey, cacheFound := s.NodePublicKeys.Get(cacheKey); cacheFound {
		return cachedKey.(*rsa.PublicKey)
	}
	node := s.GetNode(nodeId)
	if key, err := ParsePublicKey(node.PublicKey); err == nil {
		s.NodePublicKeys.Set(cacheKey, &key, cache.DefaultExpiration)
		return key
	} else {
		return nil
	}
}
