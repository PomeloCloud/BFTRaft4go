package server

import (
	"context"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"sync"
	"time"
)

func RequestVoteRequestSignData(req *pb.RequestVoteRequest) []byte {
	return []byte(fmt.Sprint(req.Group, "-", req.Term, "-", req.LogIndex, "-", req.Term, "-", req.CandidateId))
}

func RequestVoteResponseSignData(res *pb.RequestVoteResponse) []byte {
	return []byte(fmt.Sprint(res.Group, "-", res.Term, "-", res.LogIndex, "-", res.Term, "-", res.CandidateId, "-", res.Granted))
}

func ResetTerm(meta *RTGroupMeta, term uint64) {
	meta.Group.Term = term
	meta.Votes = []*pb.RequestVoteResponse{}
	meta.VotedPeer = 0
	for peerId := range meta.GroupPeers {
		meta.VotesForEntries[peerId] = true
	}
}

func (s *BFTRaftServer) BecomeCandidate(meta *RTGroupMeta) {
	RefreshTimer(meta, 10)
	meta.Role = CANDIDATE
	group := meta.Group
	ResetTerm(meta, group.Term+1)
	term := group.Term
	s.SaveGroup(meta.Group)
	lastEntry := s.LastLogEntry(group.Id)
	request := &pb.RequestVoteRequest{
		Group:       group.Id,
		Term:        term,
		LogIndex:    lastEntry.Index,
		LogTerm:     lastEntry.Term,
		CandidateId: meta.Peer,
		Signature:   []byte{},
	}
	request.Signature = s.Sign(RequestVoteRequestSignData(request))
	lock := sync.Mutex{}
	votes := 0
	voteReceived := make(chan *pb.RequestVoteResponse)
	adequateVotes := make(chan bool, 1)
	for _, peer := range meta.GroupPeers {
		nodeId := peer.Host
		if nodeId == s.Id {
			continue
		}
		node := s.GetNode(nodeId)
		go func() {
			if client, err := s.ClusterClients.Get(node.ServerAddr); err == nil {
				if voteResponse, err := client.rpc.RequestVote(context.Background(), request); err == nil {
					publicKey := s.GetNodePublicKey(nodeId)
					signData := RequestVoteResponseSignData(voteResponse)
					if VerifySign(publicKey, voteResponse.Signature, signData) == nil {
						if voteResponse.Granted && voteResponse.LogIndex <= lastEntry.Index {
							lock.Lock()
							votes++
							voteReceived <- voteResponse
							lock.Unlock()
						}
					}
				}
			}
		}()
	}
	go func() {
		// Here we can follow the rule of Raft by expecting majority votes
		// or follow the PBFT rule by expecting n - f votes
		// I will use the rule from Raft first
		expectedVotes := len(meta.GroupPeers) / 2 // ExpectedHonestPeers(s.OnboardGroupPeersSlice(group.Id))
		for vote, voted := <-voteReceived; voted; {
			if votes > expectedVotes {
				meta.Votes = append(meta.Votes, vote)
				adequateVotes <- true
				break
			}
		}
	}()
	go func() {
		select {
		case <-adequateVotes:
			s.BecomeLeader(meta)
		case <-time.After(5000 * time.Second):
			close(voteReceived)
		}
	}()
}

func (s *BFTRaftServer) BecomeLeader(meta *RTGroupMeta) {
	// when this peer become the leader of the group
	// it need to send it's vote to followers to claim it's authority
	// this only need to be done once in each term
	// so we just send the 'AppendEntry' request in this function
	// we can use a dedicated rpc protocol for this, but no bother
	meta.Role = LEADER
	meta.Group.LeaderPeer = meta.Peer // set self to leader for next following requests
	s.SaveGroup(meta.Group)
	s.SendFollowersHeartbeat(context.Background(), meta.Peer, meta.Group)
}

func (s *BFTRaftServer) BecomeFollower(meta *RTGroupMeta, appendEntryReq *pb.AppendEntriesRequest) bool {
	// first we need to verify the leader got all of the votes required
	expectedVotes := ExpectedHonestPeers(s.OnboardGroupPeersSlice(meta.Group.Id))
	if len(appendEntryReq.QuorumVotes) < expectedVotes {
		return false
	}
	votes := map[uint64]bool{}
	for _, vote := range appendEntryReq.QuorumVotes {
		votePeer, foundCandidate := meta.GroupPeers[vote.Voter]
		if !foundCandidate || vote.Term <= meta.Group.Term {
			continue
		}
		// check their signatures
		signData := RequestVoteResponseSignData(vote)
		publicKey := s.GetNodePublicKey(votePeer.Host)
		if VerifySign(publicKey, vote.Signature, signData) != nil {
			continue
		}
		// check their properties to avoid forging
		if vote.Group == meta.Group.Id && vote.CandidateId == appendEntryReq.LeaderId && vote.Granted {
			votes[votePeer.Id] = true
		}
	}
	if len(votes) > expectedVotes {
		// received enough votes, will transform to follower
		meta.Role = FOLLOWER
		meta.Group.LeaderPeer = appendEntryReq.LeaderId
		ResetTerm(meta, appendEntryReq.Term)
		s.SaveGroup(meta.Group)
		return true
	} else {
		return false
	}
}