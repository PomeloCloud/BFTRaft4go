package server

import (
	"context"
	"encoding/binary"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
)

func (s *BFTRaftServer) GetGroupPeers(group uint64) []*pb.Peer {
	cacheKey := strconv.Itoa(int(group))
	if cachedGroupPeers, cachedFound := s.GroupsPeers.Get(cacheKey); cachedFound {
		return cachedGroupPeers.([]*pb.Peer)
	}
	var peers []*pb.Peer
	keyPrefix := ComposeKeyPrefix(group, GROUP_PEERS)
	iter := s.DB.NewIterator(badger.IteratorOptions{PrefetchValues: false})
	iter.Seek(append(keyPrefix, U64Bytes(0)...)) // seek the head
	for iter.ValidForPrefix(keyPrefix) {
		item_key := iter.Item().Key()
		peer_id := BytesU64(item_key, len(keyPrefix))
		peers = append(peers, s.GetPeer(group, peer_id))
	}
	s.GroupsPeers.Set(cacheKey, peers, cache.DefaultExpiration)
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
	if data == nil {
		return nil
	} else {
		peer := pb.Peer{}
		proto.Unmarshal(*data, &peer)
		s.Peers.Set(cacheKey, &peer, cache.DefaultExpiration)
		return &peer
	}
}

func (s *BFTRaftServer) PeerUncommittedLogEntries(group *pb.RaftGroup, peer *pb.Peer) ([]*pb.LogEntry, *pb.LogEntry) {
	iter := s.ReversedLogIterator(group.Id)
	nextLogIdx := peer.NextIndex
	result := []*pb.LogEntry{}
	prevEntry := &pb.LogEntry{
		Term:  0,
		Index: 0,
	}
	for true {
		entry := iter.Next()
		if entry == nil {
			break
		}
		prevEntry = entry
		if entry.Index < nextLogIdx {
			break
		}
		result = append(result, entry)
	}
	return result, prevEntry
}

func AppendLogEntrySignData(groupId uint64, term uint64, prevIndex uint64, prevTerm uint64) []byte {
	return []byte(fmt.Sprint(groupId, "-", term, "-", prevIndex, "-", prevTerm))
}

func (s *BFTRaftServer) SendPeerUncommittedLogEntries(ctx context.Context, group *pb.RaftGroup, peer *pb.Peer) {
	node := s.GetNode(peer.Host)
	if node == nil {
		return
	}
	if client, err := s.Clients.Get(node.ServerAddr); err != nil {
		go func() {
			entries, prevEntry := s.PeerUncommittedLogEntries(group, peer)
			signData := AppendLogEntrySignData(group.Id, group.Term, prevEntry.Index, prevEntry.Term)
			client.rpc.AppendEntries(ctx, &pb.AppendEntriesRequest{
				Group:        group.Id,
				Term:         group.Term,
				LeaderId:     s.Id,
				PrevLogIndex: prevEntry.Index,
				PrevLogTerm:  prevEntry.Term,
				Signature:    s.Sign(signData),
				QuorumVotes:  []*pb.RequestVoteResponse{},
				Entries:      entries,
			})
		}()
	}
}
