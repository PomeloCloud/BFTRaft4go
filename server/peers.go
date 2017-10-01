package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"fmt"
	"github.com/patrickmn/go-cache"
)

func (s *BFTRaftServer) GetGroupPeers(group uint64) []*pb.Peer {
	var peers []*pb.Peer
	keyPrefix := ComposeKeyPrefix(group, GROUP_PEERS)
	iter := s.DB.NewIterator(badger.IteratorOptions{PrefetchValues: false})
	iter.Seek(append(keyPrefix, U64Bytes(0)...)) // seek the head
	for iter.ValidForPrefix(keyPrefix) {
		item_key := iter.Item().Key()
		peer_id := BytesU64(item_key, len(keyPrefix))
		peers = append(peers, s.GetPeer(group, peer_id))
	}
	return peers
}

func (s *BFTRaftServer) GetPeer(group uint64, peer_id uint64) *pb.Peer {
	cacheKey := fmt.Sprint(group, "-", peer_id)
	cachedPeer, cachedFound := s.Peers.Get(cacheKey)
	if cachedFound {
		return cachedPeer.(*pb.Peer)
	}
	dbKey := append(ComposeKeyPrefix(group, GROUP_PEERS), U64Bytes(peer_id)...)
	item := badger.KVItem{}
	s.DB.Get(dbKey, &item)
	data := ItemValue(&item)
	peer := pb.Peer{}
	proto.Unmarshal(data, &peer)
	s.Peers.Set(cacheKey, &peer, cache.DefaultExpiration)
	return &peer
}