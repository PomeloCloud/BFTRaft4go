package server

import (
	"crypto/rsa"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
	"log"
	"github.com/PomeloCloud/BFTRaft4go/utils"
)

type NodeIterator struct {
	prefix []byte
	data   *badger.Iterator
	server *BFTRaftServer
}

func (s *BFTRaftServer) GetHost(txn *badger.Txn, nodeId uint64) *pb.Host {
	cacheKey := strconv.Itoa(int(nodeId))
	if cacheNode, cachedFound := s.Hosts.Get(cacheKey); cachedFound {
		return cacheNode.(*pb.Host)
	}
	if item, err := txn.Get(append(ComposeKeyPrefix(HOST_LIST_GROUP, HOST_LIST), utils.U64Bytes(nodeId)...)); err == nil {
		data := ItemValue(item)
		if data == nil {
			return nil
		}
		node := pb.Host{}
		proto.Unmarshal(*data, &node)
		s.Hosts.Set(cacheKey, &node, cache.DefaultExpiration)
		return &node
	} else {
		log.Println(err)
		return nil
	}
}

func (s *BFTRaftServer) GetHostNTXN(nodeId uint64) *pb.Host {
	node := &pb.Host{}
	s.DB.View(func(txn *badger.Txn) error {
		node = s.GetHost(txn, nodeId)
		return nil
	})
	return node
}

func (s *BFTRaftServer) GetHostPublicKey(nodeId uint64) *rsa.PublicKey {
	cacheKey := strconv.Itoa(int(nodeId))
	if cachedKey, cacheFound := s.NodePublicKeys.Get(cacheKey); cacheFound {
		return cachedKey.(*rsa.PublicKey)
	}
	node := s.GetHostNTXN(nodeId)
	if node == nil {
		return nil
	}
	if key, err := utils.ParsePublicKey(node.PublicKey); err == nil {
		s.NodePublicKeys.Set(cacheKey, key, cache.DefaultExpiration)
		return key
	} else {
		return nil
	}
}

func (s *BFTRaftServer) SaveHost(txn *badger.Txn, node *pb.Host) error {
	if data, err := proto.Marshal(node); err == nil {
		dbKey := append(ComposeKeyPrefix(HOST_LIST_GROUP, HOST_LIST), utils.U64Bytes(node.Id)...)
		return txn.Set(dbKey, data, 0x00)
	} else {
		return err
	}
}

func (s *BFTRaftServer) SaveHostNTXN(node *pb.Host) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		return s.SaveHost(txn, node)
	})
}

func (s *BFTRaftServer) VerifyCommandSign(cmd *pb.CommandRequest) bool {
	signData := utils.ExecCommandSignData(cmd)
	publicKey := s.GetHostPublicKey(cmd.ClientId)
	if publicKey == nil {
		return false
	} else {
		return utils.VerifySign(publicKey, cmd.Signature, signData) == nil
	}
}