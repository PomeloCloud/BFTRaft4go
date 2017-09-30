package server

import (
	"encoding/binary"
	"github.com/dgraph-io/badger"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"github.com/golang/protobuf/proto"
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

func (s *BFTRaftServer) GroupPeers(group uint64) ([]pb.Peer, error) {
	var peers []pb.Peer
	keyPrefix := ComposeKeyPrefix(group, GROUP_PEERS)
	iter := s.DB.NewIterator(badger.IteratorOptions{})
	iter.Seek(append(keyPrefix, U64Bytes(0)...)) // seek the head
	for iter.ValidForPrefix(keyPrefix) {
		peer := pb.Peer{}
		data := ItemValue(iter.Item())
		proto.Unmarshal(data, &peer)
		peers = append(peers, peer)
	}
	return peers, nil
}

func (s *BFTRaftServer) ReversedLogIterator (group uint64) LogEntryIterator {
	keyPrefix := ComposeKeyPrefix(group, RAFT_LOGS)
	iter := s.DB.NewIterator(badger.IteratorOptions{Reverse: true})
	iter.Seek(append(keyPrefix, U64Bytes(^0)...)) // search from max possible index
	return LogEntryIterator{
		prefix: keyPrefix,
		data: iter,
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