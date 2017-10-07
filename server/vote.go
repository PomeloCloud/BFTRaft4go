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

func (s *BFTRaftServer) BecomeCandidate(meta *RTGroupMeta) {
	RefreshTimer(meta, 10)
	meta.Role = CANDIDATE
	group := meta.Group
	group.Term++
	term := group.Term
	s.SaveGroup(meta.Group)
	meta.Votes = []*pb.RequestVoteResponse{}
	meta.NewTerm = true
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
		expectedVotes := len(meta.GroupPeers) / 2 // ExpectedHonestPeers(s.GroupPeersSlice(group.Id))
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
	s.SendFollowersHeartbeat(context.Background(), meta.Peer, meta.Group)
}
