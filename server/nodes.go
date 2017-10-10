package server

import (
	"crypto/rsa"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
	"log"
)

type NodeIterator struct {
	prefix []byte
	data   *badger.Iterator
	server *BFTRaftServer
}

func (s *BFTRaftServer) GetNode(txn *badger.Txn, nodeId uint64) *pb.Node {
	cacheKey := strconv.Itoa(int(nodeId))
	if cacheNode, cachedFound := s.Nodes.Get(cacheKey); cachedFound {
		return cacheNode.(*pb.Node)
	}
	if item, err := txn.Get(append(ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST), U64Bytes(nodeId)...)); err == nil {
		data := ItemValue(item)
		if data == nil {
			return nil
		}
		node := pb.Node{}
		proto.Unmarshal(*data, &node)
		s.Nodes.Set(cacheKey, &node, cache.DefaultExpiration)
		return &node
	} else {
		log.Println(err)
		return nil
	}
}

func (s *BFTRaftServer) GetNodeNTXN(nodeId uint64) *pb.Node {
	node := &pb.Node{}
	s.DB.View(func(txn *badger.Txn) error {
		node = s.GetNode(txn, nodeId)
		return nil
	})
	return node
}

func (s *BFTRaftServer) GetNodePublicKey(nodeId uint64) *rsa.PublicKey {
	cacheKey := strconv.Itoa(int(nodeId))
	if cachedKey, cacheFound := s.NodePublicKeys.Get(cacheKey); cacheFound {
		return cachedKey.(*rsa.PublicKey)
	}
	node := s.GetNodeNTXN(nodeId)
	if key, err := ParsePublicKey(node.PublicKey); err == nil {
		s.NodePublicKeys.Set(cacheKey, &key, cache.DefaultExpiration)
		return key
	} else {
		return nil
	}
}

func (s *BFTRaftServer) SaveNode(txn *badger.Txn, node *pb.Node) error {
	if data, err := proto.Marshal(node); err == nil {
		dbKey := append(ComposeKeyPrefix(NODE_LIST_GROUP, NODES_LIST), U64Bytes(node.Id)...)
		return txn.Set(dbKey, data, 0x00)
	} else {
		return err
	}
}