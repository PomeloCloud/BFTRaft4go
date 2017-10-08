package server

import (
	"context"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"sync"
	"github.com/PomeloCloud/BFTRaft4go/utils"
)

func GetGroupPeersFromKV(group uint64, KV *badger.KV) map[uint64]*pb.Peer {
	var peers map[uint64]*pb.Peer
	keyPrefix := ComposeKeyPrefix(group, GROUP_PEERS)
	iter := KV.NewIterator(badger.IteratorOptions{})
	iter.Seek(append(keyPrefix, U64Bytes(0)...)) // seek the head
	for iter.ValidForPrefix(keyPrefix) {
		item := iter.Item()
		item_data := ItemValue(item)
		peer := pb.Peer{}
		proto.Unmarshal(*item_data, &peer)
		peers[peer.Id] = &peer
	}
	iter.Close()
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
	entries := []*pb.LogEntry{}
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
		entries = append(entries, entry)
	}
	// reverse so the first will be the one with least index
	if len(entries) > 1 {
		for i := 0; i < len(entries)/2; i++ {
			j := len(entries) - i - 1
			entries[i], entries[j] = entries[j], entries[i]
		}
	}
	return entries, prevEntry
}

func (s *BFTRaftServer) SendPeerUncommittedLogEntries(ctx context.Context, group *pb.RaftGroup, peer *pb.Peer) {
	node := s.GetNode(peer.Host)
	meta := s.GroupsOnboard[group.Id]
	if node == nil {
		return
	}
	if client, err := utils.GetClusterRPC(node.ServerAddr); err != nil {
		votes := []*pb.RequestVoteResponse{}
		if meta.VotesForEntries[meta.Peer] {
			votes = meta.Votes
		}
		entries, prevEntry := s.PeerUncommittedLogEntries(group, peer)
		signData := AppendLogEntrySignData(group.Id, group.Term, prevEntry.Index, prevEntry.Term)
		appendResult, err := client.AppendEntries(ctx, &pb.AppendEntriesRequest{
			Group:        group.Id,
			Term:         group.Term,
			LeaderId:     peer.Id,
			PrevLogIndex: prevEntry.Index,
			PrevLogTerm:  prevEntry.Term,
			Signature:    s.Sign(signData),
			QuorumVotes:  votes,
			Entries:      entries,
		})
		if err == nil {
			if VerifySign(s.GetNodePublicKey(node.Id), appendResult.Signature, appendResult.Hash) != nil {
				return
			}
			var lastEntry *pb.LogEntry
			if len(entries) == 0 {
				lastEntry = prevEntry
			} else {
				lastEntry = entries[len(entries)-1]
			}
			if appendResult.Index <= lastEntry.Index && appendResult.Term <= lastEntry.Term {
				peer.MatchIndex = appendResult.Index
				peer.NextIndex = peer.MatchIndex + 1
				s.SavePeer(peer)
			}
			meta.VotesForEntries[meta.Peer] = appendResult.Convinced
		}
	}
}

func (s *BFTRaftServer) GroupServerPeer(groupId uint64) *pb.Peer {
	if groupMeta, found := s.GroupsOnboard[groupId]; found {
		return s.GetPeer(groupId, groupMeta.Peer)
	} else {
		return nil
	}
}

func ScanHostedGroups(kv *badger.KV, serverId uint64) map[uint64]*RTGroupMeta {
	scanKey := U64Bytes(GROUP_PEERS)
	iter := kv.NewIterator(badger.IteratorOptions{})
	iter.Seek(scanKey)
	groups := map[uint64]*RTGroupMeta{}
	for iter.ValidForPrefix(scanKey) {
		item := iter.Item()
		val := ItemValue(item)
		peer := &pb.Peer{}
		proto.Unmarshal(*val, peer)
		if peer.Host == serverId {
			group := GetGroupFromKV(peer.Group, kv)
			if group != nil {
				groups[peer.Group] = &RTGroupMeta{
					Peer:       peer.Id,
					Leader:     group.LeaderPeer,
					Lock:       sync.RWMutex{},
					GroupPeers: GetGroupPeersFromKV(peer.Group, kv),
					Group:      group,
				}
			}
		}
	}
	iter.Close()
	return groups
}

func (s *BFTRaftServer) OnboardGroupPeersSlice(groupId uint64) []*pb.Peer {
	peers := []*pb.Peer{}
	for _, peer := range s.GroupsOnboard[groupId].GroupPeers {
		peers = append(peers, peer)
	}
	return peers
}

func (s *BFTRaftServer) SavePeer(peer *pb.Peer) {
	if data, err := proto.Marshal(peer); err == nil {
		dbKey := append(ComposeKeyPrefix(peer.Group, GROUP_PEERS), U64Bytes(peer.Id)...)
		s.DB.Set(dbKey, data, 0x00)
	}
}
