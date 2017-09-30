package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
)

func (s *BFTRaftServer) GetGroupPeers(group uint64) []pb.Peer {
	s.lock.RLock()
	if peers, found := s.Peers[group]; found {
		s.lock.RUnlock()
		return peers
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if peers, found := s.Peers[group]; found {
		return peers
	}
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
	s.Peers[group] = peers
	return peers
}