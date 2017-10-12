package server

import (
	"context"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"github.com/tevino/abool"
	"log"
	"sync"
)

func GetGroupPeersFromKV(txn *badger.Txn, group uint64) map[uint64]*pb.Peer {
	peers := map[uint64]*pb.Peer{}
	keyPrefix := ComposeKeyPrefix(group, GROUP_PEERS)
	iter := txn.NewIterator(badger.IteratorOptions{})
	iter.Seek(append(keyPrefix, utils.U64Bytes(0)...)) // seek the head
	for iter.ValidForPrefix(keyPrefix) {
		item := iter.Item()
		item_data := ItemValue(item)
		peer := pb.Peer{}
		proto.Unmarshal(*item_data, &peer)
		peers[peer.Id] = &peer
		iter.Next()
	}
	iter.Close()
	return peers
}

func (s *BFTRaftServer) GetPeer(txn *badger.Txn, group uint64, peer_id uint64) *pb.Peer {
	cacheKey := fmt.Sprint(group, "-", peer_id)
	cachedPeer, cachedFound := s.Peers.Get(cacheKey)
	if cachedFound {
		return cachedPeer.(*pb.Peer)
	}
	dbKey := append(ComposeKeyPrefix(group, GROUP_PEERS), utils.U64Bytes(peer_id)...)
	item, _ := txn.Get(dbKey)
	data := ItemValue(item)
	if data == nil {
		return nil
	} else {
		peer := pb.Peer{}
		proto.Unmarshal(*data, &peer)
		s.Peers.Set(cacheKey, &peer, cache.DefaultExpiration)
		return &peer
	}
}

func (s *BFTRaftServer) GetPeerNTXN(group uint64, peer_id uint64) *pb.Peer {
	peer := &pb.Peer{}
	s.DB.View(func(txn *badger.Txn) error {
		peer = s.GetPeer(txn, group, peer_id)
		return nil
	})
	return peer
}

func (s *BFTRaftServer) PeerUncommittedLogEntries(group *pb.RaftGroup, peer *pb.Peer) ([]*pb.LogEntry, *pb.LogEntry) {
	entries_ := []*pb.LogEntry{}
	prevEntry := &pb.LogEntry{
		Term:  0,
		Index: 0,
	}
	s.DB.View(func(txn *badger.Txn) error {
		entries := []*pb.LogEntry{}
		iter := s.ReversedLogIterator(txn, group.Id)
		nextLogIdx := peer.NextIndex
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
		entries_ = entries
		return nil
	})
	return entries_, prevEntry
}

func (s *BFTRaftServer) SendPeerUncommittedLogEntries(ctx context.Context, group *pb.RaftGroup, peer *pb.Peer) {
	node := s.GetHostNTXN(peer.Host)
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
			if utils.VerifySign(s.GetHostPublicKey(node.Id), appendResult.Signature, appendResult.Hash) != nil {
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
				if s.DB.Update(func(txn *badger.Txn) error {
					return s.SavePeer(txn, peer)
				}) != nil {
					log.Println(err)
				}
			}
			meta.VotesForEntries[meta.Peer] = appendResult.Convinced
		}
	}
}

func (s *BFTRaftServer) GroupServerPeer(txn *badger.Txn, groupId uint64) *pb.Peer {
	if groupMeta, found := s.GroupsOnboard[groupId]; found {
		return s.GetPeer(txn, groupId, groupMeta.Peer)
	} else {
		return nil
	}
}

func (s *BFTRaftServer) GroupServerPeerNTXN(groupId uint64) *pb.Peer {
	peer := &pb.Peer{}
	s.DB.View(func(txn *badger.Txn) error {
		peer = s.GroupServerPeer(txn, groupId)
		return nil
	})
	return peer
}

func ScanHostedGroups(db *badger.DB, serverId uint64) map[uint64]*RTGroupMeta {
	scanKey := utils.U64Bytes(GROUP_PEERS)
	res := map[uint64]*RTGroupMeta{}
	db.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.IteratorOptions{})
		iter.Seek(scanKey)
		groups := map[uint64]*RTGroupMeta{}
		for iter.ValidForPrefix(scanKey) {
			item := iter.Item()
			val := ItemValue(item)
			peer := &pb.Peer{}
			proto.Unmarshal(*val, peer)
			if peer.Host == serverId {
				group := GetGroupFromKV(txn, peer.Group)
				if group != nil {
					groups[peer.Group] = &RTGroupMeta{
						Peer:       peer.Id,
						Leader:     group.LeaderPeer,
						Lock:       sync.RWMutex{},
						GroupPeers: GetGroupPeersFromKV(txn, peer.Group),
						Group:      group,
						IsBusy:     abool.NewBool(false),
					}
				}
			}
		}
		iter.Close()
		groups = res
		return nil
	})
	return res
}

func (s *BFTRaftServer) OnboardGroupPeersSlice(groupId uint64) []*pb.Peer {
	peers := []*pb.Peer{}
	for _, peer := range s.GroupsOnboard[groupId].GroupPeers {
		peers = append(peers, peer)
	}
	return peers
}

func (s *BFTRaftServer) SavePeer(txn *badger.Txn, peer *pb.Peer) error {
	if data, err := proto.Marshal(peer); err == nil {
		dbKey := append(ComposeKeyPrefix(peer.Group, GROUP_PEERS), utils.U64Bytes(peer.Id)...)
		return txn.Set(dbKey, data, 0x00)
	} else {
		return err
	}
}
