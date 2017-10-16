package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)

func (m *RTGroup) prepareApproveChan(groupId uint64, logIndex uint64) {
	//cache_key := fmt.Sprint(logIndex)
	//if _, existed := s.GroupAppendedLogs.Get(cache_key); !existed {
	//	s.GroupAppendedLogs.Set(cache_key, make(chan bool, 1), cache.DefaultExpiration)
	//}
}

func (m *RTGroup) WaitLogApproved(logIndex uint64) bool {
	// TODO: fix wait approved
	//s.prepareApproveChan(groupId, logIndex)
	//cache_key := fmt.Sprint(groupId, "-", logIndex)
	//cache_chan, _ := s.GroupAppendedLogs.Get(cache_key)
	//select {
	//case approved := <-cache_chan.(chan bool):
	//	return approved
	//case <-time.After(s.Opts.ConsensusTimeout):
	//	log.Println("wait apprival timeout, group:", groupId, "log:", logIndex)
	//	return false
	//}
	return true
}

func (m *RTGroup) SetLogAppended(groupId uint64, logIndex uint64, isApproved bool) {
	//m.prepareApproveChan(groupId, logIndex)
	//cache_key := fmt.Sprint(groupId, "-", logIndex)
	//if c, existed := m.GroupAppendedLogs.Get(cache_key); existed {
	//	go func() {
	//		c.(chan bool) <- isApproved
	//	}()
	//}
}

func ExpectedHonestPeers(group_peers []*pb.Peer) int {
	num_peers := len(group_peers)
	return num_peers - num_peers/2
}

func (s *BFTRaftServer) PeerApprovedAppend(groupId uint64, logIndex uint64, peer uint64, group_peers []*pb.Peer, isApproved bool) {
	//cache_key := fmt.Sprint(groupId, "-", logIndex)
	//if _, existed := s.GroupApprovedLogs.Get(cache_key); !existed {
	//	s.GroupApprovedLogs.Set(cache_key, map[uint64]bool{}, cache.DefaultExpiration)
	//}
	//approvedPeers, _ := s.GroupApprovedLogs.Get(cache_key)
	//approvedPeersMap := approvedPeers.(map[uint64]bool)
	//approvedPeersMap[peer] = isApproved
	//expectedVotes := ExpectedHonestPeers(group_peers)
	//if len(approvedPeersMap) >= expectedVotes {
	//	approvedVotes := 0
	//	rejectedVotes := 0
	//	for _, vote := range approvedPeersMap {
	//		if vote {
	//			approvedVotes++
	//		} else {
	//			rejectedVotes++
	//		}
	//	}
	//	if approvedVotes >= expectedVotes {
	//		s.SetLogAppended(groupId, logIndex, true)
	//		return
	//	}
	//	if rejectedVotes >= (len(group_peers) - expectedVotes) {
	//		s.SetLogAppended(groupId, logIndex, false)
	//		return
	//	}
	//}
}
