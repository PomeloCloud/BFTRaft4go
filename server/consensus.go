package server

import (
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/patrickmn/go-cache"
	"time"
)

func (s *BFTRaftServer) prepareApproveChan(groupId uint64, logIndex uint64) {
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	if _, existed := s.GroupAppendedLogs.Get(cache_key); !existed {
		s.GroupAppendedLogs.Set(cache_key, make(chan bool, 1), cache.DefaultExpiration)
	}
}

func (s *BFTRaftServer) WaitLogApproved(groupId uint64, logIndex uint64) bool {
	s.prepareApproveChan(groupId, logIndex)
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	cache_chan, _ := s.GroupAppendedLogs.Get(cache_key)
	select {
	case approved := <-cache_chan.(chan bool):
		return approved
	case <-time.After(s.Opts.ConsensusTimeout):
		return false
	}
}

func (s *BFTRaftServer) SetLogAppended(groupId uint64, logIndex uint64, isApproved bool) {
	s.prepareApproveChan(groupId, logIndex)
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	if c, existed := s.GroupAppendedLogs.Get(cache_key); existed {
		go func() {
			c.(chan bool) <- isApproved
		}()
	}
}

func ExpectedHonestPeers(group_peers []*pb.Peer) int {
	num_peers := len(group_peers)
	return num_peers - num_peers/2
}

func (s *BFTRaftServer) PeerApprovedAppend(groupId uint64, logIndex uint64, peer uint64, group_peers []*pb.Peer, isApproved bool) {
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	if _, existed := s.GroupApprovedLogs.Get(cache_key); !existed {
		s.GroupApprovedLogs.Set(cache_key, map[uint64]bool{}, cache.DefaultExpiration)
	}
	approvedPeers, _ := s.GroupApprovedLogs.Get(cache_key)
	approvedPeersMap := approvedPeers.(map[uint64]bool)
	approvedPeersMap[peer] = isApproved
	expectedVotes := ExpectedHonestPeers(group_peers)
	if len(approvedPeersMap) >= expectedVotes {
		approvedVotes := 0
		rejectedVotes := 0
		for _, vote := range approvedPeersMap {
			if vote {
				approvedVotes++
			} else {
				rejectedVotes++
			}
		}
		if approvedVotes >= expectedVotes {
			s.SetLogAppended(groupId, logIndex, true)
			return
		}
		if rejectedVotes >= (len(group_peers) - expectedVotes) {
			s.SetLogAppended(groupId, logIndex, false)
			return
		}
	}
}
