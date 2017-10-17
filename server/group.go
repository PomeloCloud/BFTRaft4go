package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/90TechSAS/go-recursive-mutex"
	cpb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/huandu/goroutine"
	"github.com/patrickmn/go-cache"
	"github.com/tevino/abool"
	"log"
	"math/rand"
	"strconv"
	"time"
)

const (
	LEADER    = 0
	FOLLOWER  = 1
	CANDIDATE = 2
	OBSERVER  = 3
)

type RTGroup struct {
	Server            *BFTRaftServer
	Leader            uint64
	VotedPeer         uint64
	GroupPeers        map[uint64]*pb.Peer
	Group             *pb.RaftGroup
	Timeout           time.Time
	Role              int
	Votes             []*pb.RequestVoteResponse
	SendVotesForPeers map[uint64]bool // key is peer id
	IsBusy            *abool.AtomicBool
	Lock              recmutex.RecursiveMutex
}

func NewRTGroup(
	server *BFTRaftServer,
	leader uint64,
	groupPeers map[uint64]*pb.Peer,
	group *pb.RaftGroup, role int,
) *RTGroup {
	meta := &RTGroup{
		Server:            server,
		Leader:            leader,
		VotedPeer:         0,
		GroupPeers:        groupPeers,
		Group:             group,
		Timeout:           time.Now().Add(20 * time.Second),
		Role:              role,
		Votes:             []*pb.RequestVoteResponse{},
		SendVotesForPeers: map[uint64]bool{},
		IsBusy:            abool.NewBool(false),
		Lock:              recmutex.RecursiveMutex{},
	}
	meta.StartTimeWheel()
	return meta
}

func GetGroupFromKV(txn *badger.Txn, groupId uint64) *pb.RaftGroup {
	group := &pb.RaftGroup{}
	keyPrefix := ComposeKeyPrefix(groupId, GROUP_META)
	if item, err := txn.Get(keyPrefix); err == nil {
		data := ItemValue(item)
		if data == nil {
			return nil
		} else {
			proto.Unmarshal(*data, group)
			return group
		}
	} else {
		return nil
	}
}

func (s *BFTRaftServer) GetGroup(txn *badger.Txn, groupId uint64) *pb.RaftGroup {
	cacheKey := strconv.Itoa(int(groupId))
	cachedGroup, cacheFound := s.Groups.Get(cacheKey)
	if cacheFound {
		return cachedGroup.(*pb.RaftGroup)
	} else {
		group := GetGroupFromKV(txn, groupId)
		if group != nil {
			s.Groups.Set(cacheKey, group, cache.DefaultExpiration)
			return group
		} else {
			return nil
		}
	}
}

func (s *BFTRaftServer) GetGroupNTXN(groupId uint64) *pb.RaftGroup {
	group := &pb.RaftGroup{}
	s.DB.View(func(txn *badger.Txn) error {
		group = s.GetGroup(txn, groupId)
		return nil
	})
	return group
}

func (s *BFTRaftServer) SaveGroup(txn *badger.Txn, group *pb.RaftGroup) error {
	if data, err := proto.Marshal(group); err == nil {
		dbKey := ComposeKeyPrefix(group.Id, GROUP_META)
		return txn.Set(dbKey, data, 0x00)
	} else {
		return err
	}
}

func (s *BFTRaftServer) SaveGroupNTXN(group *pb.RaftGroup) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		return s.SaveGroup(txn, group)
	})
}

func (s *BFTRaftServer) GetGroupHosts(txn *badger.Txn, groupId uint64) []*pb.Host {
	nodes := []*pb.Host{}
	peers := GetGroupPeersFromKV(txn, groupId)
	for _, peer := range peers {
		node := s.GetHost(txn, peer.Id)
		if node != nil {
			nodes = append(nodes, node)
		} else {
			log.Println(s.Id, "cannot find group node:", peer.Id)
		}
	}
	return nodes
}

func (s *BFTRaftServer) GetGroupHostsNTXN(groupId uint64) []*pb.Host {
	result := []*pb.Host{}
	s.DB.View(func(txn *badger.Txn) error {
		result = s.GetGroupHosts(txn, groupId)
		return nil
	})
	return result
}

func (s *BFTRaftServer) GroupLeader(groupId uint64) *pb.GroupLeader {
	res := &pb.GroupLeader{}
	if meta := s.GetOnboardGroup(groupId); meta != nil {
		node := s.GetHostNTXN(meta.Leader)
		if node == nil {
			log.Println("cannot get node for group leader")
		}
		res = &pb.GroupLeader{
			Node:    node,
			Accuate: true,
		}
	} else {
		// group not on the host
		// will select a host randomly in the group
		res := &pb.GroupLeader{
			Accuate: false,
		}
		s.DB.View(func(txn *badger.Txn) error {
			hosts := s.GetGroupHosts(txn, groupId)
			if len(hosts) > 0 {
				res.Node = hosts[rand.Intn(len(hosts))]
			} else {
				log.Println("cannot get group leader")
			}
			return nil
		})
	}
	return res
}

func (s *BFTRaftServer) GetOnboardGroup(id uint64) *RTGroup {
	k := strconv.Itoa(int(id))
	if meta, found := s.GroupsOnboard.Get(k); found {
		return meta.(*RTGroup)
	} else {
		return nil
	}
}

func (s *BFTRaftServer) SetOnboardGroup(meta *RTGroup) {
	k := strconv.Itoa(int(meta.Group.Id))
	if meta == nil {
		panic("group is nil")
	}
	s.GroupsOnboard.Set(k, meta)
}

func (m *RTGroup) RefreshTimer(mult float32) {
	m.Timeout = time.Now().Add(time.Duration(RandomTimeout(mult)) * time.Millisecond)
}

func (m *RTGroup) StartTimeWheel() {
	go func() {
		for true {
			if m.Timeout.After(time.Now()) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			m.Lock.Lock()
			if m.Role == FOLLOWER {
				if m.Leader == m.Server.Id {
					panic("Follower is leader")
				}
				// not leader
				log.Println(m.Server.Id, "is candidate")
				m.BecomeCandidate()
			} else if m.Role == LEADER {
				// is leader, send heartbeat
				m.SendFollowersHeartbeat(context.Background())
			} else if m.Role == CANDIDATE {
				// is candidate but vote expired, start a new vote term
				log.Println(m.Group.Id, "started a new election")
				m.BecomeCandidate()
			} else if m.Role == OBSERVER {
				// update local data
				m.PullAndCommitGroupLogs()
				m.RefreshTimer(5)
			}
			m.Lock.Unlock()
		}
	}()
}

func (m *RTGroup) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	group := m.Group
	groupId := group.Id
	reqLeaderId := req.LeaderId
	leaderPeer := m.GroupPeers[reqLeaderId]
	lastLogHash := m.LastEntryHashNTXN()
	// log.Println("append log from", req.LeaderId, "to", s.Id, "entries:", len(req.Entries))
	lastEntryIndex := m.LastEntryIndexNTXN()
	log.Println("has last entry index:", lastEntryIndex, "for", groupId)
	response := &pb.AppendEntriesResponse{
		Group:     m.Group.Id,
		Term:      group.Term,
		Index:     lastEntryIndex,
		Successed: false,
		Convinced: false,
		Hash:      lastLogHash,
		Signature: m.Server.Sign(lastLogHash),
		Peer:      m.Server.Id,
	}
	// verify group and leader existence
	if group == nil || leaderPeer == nil {
		return nil, errors.New("host or group not existed on append entries")
	}
	// check leader transfer
	if len(req.QuorumVotes) > 0 && req.LeaderId != m.Leader {
		if !m.BecomeFollower(req) {
			return nil, errors.New("cannot become a follower when append entries due to votes")
		}
	} else if req.LeaderId != m.Leader {
		return nil, errors.New("leader not matches when append entries")
	}
	response.Convinced = true
	// verify signature
	if leaderPublicKey := m.Server.GetHostPublicKey(m.Leader); leaderPublicKey != nil {
		signData := AppendLogEntrySignData(group.Id, group.Term, req.PrevLogIndex, req.PrevLogTerm)
		if err := utils.VerifySign(leaderPublicKey, req.Signature, signData); err != nil {
			// log.Println("leader signature not right when append entries:", err)
			// TODO: Fix signature verification, crypto/rsa: verification error
			// return response, nil
		}
	} else {
		return nil, errors.New("cannot get leader public key when append entries")
	}
	m.Timeout = time.Now().Add(10 * time.Second)
	if len(req.Entries) > 0 {
		log.Println("appending new entries for:", m.Group.Id, "total", len(req.Entries))
		// check last log matches the first provided by the leader
		// this strategy assumes split brain will never happened (on internet)
		// the leader will always provide the entries no more than it needed
		// if the leader failed to provide the right first entries, the follower
		// 	 will not commit the log but response with current log index and term instead
		// the leader should response immediately for failed follower response
		lastLogIdx := m.LastEntryIndexNTXN()
		nextLogIdx := lastLogIdx + 1
		lastLog := m.LastLogEntryNTXN()
		lastLogTerm := uint64(0)
		if lastLog != nil {
			lastLogTerm = lastLog.Term
		}
		if req.PrevLogIndex == lastLogIdx && req.Entries[0].Index == nextLogIdx { // index matched
			if req.PrevLogTerm != lastLogTerm {
				// log mismatch, cannot preceded
				// what to do next will leave to the leader
				err := fmt.Sprint("cannot get leader public key when append entries", req.PrevLogTerm, lastLogTerm)
				return nil, errors.New(err)
			} else {
				// first log matched
				// but we still need to check hash for upcoming logs
				expectedHash := m.LastEntryHashNTXN()
				for i := nextLogIdx; i < nextLogIdx+uint64(len(req.Entries)); i++ {
					entry := req.Entries[i-nextLogIdx]
					cmd := entry.Command
					expectedHash, _ = utils.LogHash(expectedHash, i, cmd.FuncId, cmd.Arg)
					if entry.Index != i || !bytes.Equal(entry.Hash, expectedHash) {
						log.Println("log mismatch", entry.Index, "-", i, ",", entry.Hash, "-", expectedHash)
						return response, nil
					}
					if !m.Server.VerifyCommandSign(entry.Command) {
						// TODO: fix signature verification
						// log.Println("log verification failed")
						// return response, nil
					}
				}
				// here start the loop of sending approve request to all peers
				// the followers may have multiply uncommitted entries so we need to
				//		approve them one by one and wait their response for confirmation.
				//		this is crucial to ensure log correctness and updated
				// to respond to 'ApproveAppend' RPC, the server should response with:
				// 		1. appended if the server have already committed the entry
				// 			(this follower was fallen behind)
				//		2. delayed if the server is also waiting for other peers to confirm or
				// 			it is not yet reach to this log index
				//		3. failed if the group/peer/signature check was failed
				// so there is 2 way to make the append confirmation
				//		1. through the 'ApprovedAppend' from other peers
				//		2. through the appended 'ApproveAppendResponse' for catch up
				//groupPeers := s.OnboardGroupPeersSlice(groupId)
				for _, entry := range req.Entries {
					log.Println("trying to append log", entry.Index, "for group", groupId, "total", len(req.Entries))
					//for _, peer := range groupPeers {
					//	if peer.Host == s.Id {
					//		continue
					//	}
					//	if node := s.GetHostNTXN(peer.Host); node != nil {
					//		if client, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
					//			go func() {
					//				log.Println("ask others for append approval for group:", groupId, "index:", entry.Index)
					//				if approveRes, err := client.ApproveAppend(ctx, response); err == nil {
					//					if err := utils.VerifySign(
					//						s.GetHostPublicKey(node.Id),
					//						approveRes.Signature,
					//						ApproveAppendSignData(approveRes),
					//					); err == nil {
					//						if approveRes.Appended && !approveRes.Delayed && !approveRes.Failed {
					//							// log.Println("node", peer.Host, "approved", approveRes)
					//							s.PeerApprovedAppend(groupId, entry.Index, peer.Id, groupPeers, true)
					//						} else {
					//							log.Println("node", peer.Host, "returned approval", approveRes)
					//						}
					//					} else {
					//						log.Println("error on verify approve signature")
					//					}
					//				}
					//			}()
					//		}
					//	} else {
					//		if node == nil {
					//			log.Println("cannot get node ", peer.Host, " for send approval append logs")
					//		}
					//	}
					//}
					if m.WaitLogApproved(entry.Index) {
						m.Server.DB.Update(func(txn *badger.Txn) error {
							m.AppendEntryToLocal(txn, entry)
							return nil
						})
					}
					clientId := entry.Command.ClientId
					result := m.CommitGroupLog(entry)
					client := m.Server.GetHostNTXN(clientId)
					if client != nil {
						if rpc, err := m.Server.ClientRPCs.Get(client.ServerAddr); err == nil {
							nodeId := m.Server.Id
							reqId := entry.Command.RequestId
							signData := utils.CommandSignData(m.Group.Id, nodeId, reqId, *result)
							if _, err := rpc.rpc.ResponseCommand(ctx, &cpb.CommandResult{
								Group:     groupId,
								NodeId:    nodeId,
								RequestId: reqId,
								Result:    *result,
								Signature: m.Server.Sign(signData),
							}); err != nil {
								log.Println("cannot response command to ", clientId, ":", err)
							}
						}
						response.Term = entry.Term
						response.Index = entry.Index
						response.Hash = entry.Hash
						response.Signature = m.Server.Sign(entry.Hash)
					} else {
						log.Println("cannot get node", entry.Command.ClientId, "for response command")
					}
					log.Println("done appending log", entry.Index, "for group", groupId, "total", len(req.Entries))
				}
				response.Successed = true
			}
		} else {
			log.Println(
				m.Server.Id, "log mismatch: prev index",
				req.PrevLogIndex, "-", lastLogIdx,
				"next index", req.Entries[0].Index, "-", nextLogIdx,
			)
		}
	}
	log.Println("report back to leader index:", response.Index)
	return response, nil
}

func (m *RTGroup) SendFollowersHeartbeat(ctx context.Context) {
	m.Lock.Lock()
	defer m.Lock.Unlock()
	m.RefreshTimer(1)
	num_peers := len(m.GroupPeers)
	completion := make(chan *pb.AppendEntriesResponse, num_peers)
	sentMsgs := 0
	uncommittedEntries := map[uint64][]*pb.LogEntry{}
	peerPrevEntry := map[uint64]*pb.LogEntry{}
	for peerId, peer := range m.GroupPeers {
		if peerId != m.Server.Id {
			entries, prevEntry := m.PeerUncommittedLogEntries(peer)
			uncommittedEntries[peerId] = entries
			peerPrevEntry[peerId] = prevEntry
			node := m.Server.GetHostNTXN(peerId)
			if node == nil {
				log.Println("cannot get node for send peer uncommitted log entries")
				completion <- nil
				return
			}
			votes := []*pb.RequestVoteResponse{}
			if m.SendVotesForPeers[m.Server.Id] {
				votes = m.Votes
			}
			signData := AppendLogEntrySignData(m.Group.Id, m.Group.Term, prevEntry.Index, prevEntry.Term)
			signature := m.Server.Sign(signData)
			if client, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
				sentMsgs++
				go func() {
					if appendResult, err := client.AppendEntries(ctx, &pb.AppendEntriesRequest{
						Group:        m.Group.Id,
						Term:         m.Group.Term,
						LeaderId:     m.Server.Id,
						PrevLogIndex: prevEntry.Index,
						PrevLogTerm:  prevEntry.Term,
						Signature:    signature,
						QuorumVotes:  votes,
						Entries:      entries,
					}); err == nil {
						// WARN: the result may not from the peer we requested
						completion <- appendResult
					} else {
						log.Println("append log failed:", err)
						completion <- nil
					}

				}()
			} else {
				log.Println("error on append entry logs to followers:", err)
			}
		}
	}
	log.Println("sending log to", sentMsgs, "followers with", num_peers, "peers")
	for i := 0; i < sentMsgs; i++ {
		response := <-completion
		if response == nil {
			log.Println("append entry response is nil")
			continue
		}
		peerId := response.Peer
		peer := m.GroupPeers[peerId]
		publicKey := m.Server.GetHostPublicKey(peerId)
		if publicKey == nil {
			log.Println("cannot find public key for:", peerId)
			continue
		}
		if err := utils.VerifySign(publicKey, response.Signature, response.Hash); err != nil {
			// TODO: fix signature verification
			// log.Println("cannot verify append response signature:", err)
			// continue
		}
		log.Println(peerId, "append response with last index:", response.Index)
		if response.Index != peer.MatchIndex {
			log.Println(
				"#", goroutine.GoroutineId(),
				"peer:", peer.Id, "index changed:", peer.MatchIndex, "->", response.Index)
			peer.MatchIndex = response.Index
			peer.NextIndex = peer.MatchIndex + 1
			m.GroupPeers[peer.Id] = peer
			if err := m.Server.DB.Update(func(txn *badger.Txn) error {
				return m.Server.SavePeer(txn, peer)
			}); err != nil {
				log.Println("cannot save peer:", peer.Id, err)
			}
		} else {
			// log.Println(peer.Id, "last index unchanged:", response.Index, "-", peer.MatchIndex)
		}
		m.SendVotesForPeers[peerId] = !response.Convinced
	}
}

func (m *RTGroup) ApproveAppend(ctx context.Context, req *pb.AppendEntriesResponse) (*pb.ApproveAppendResponse, error) {
	groupId := req.Group
	response := &pb.ApproveAppendResponse{
		Group:     groupId,
		Peer:      m.Server.Id,
		Index:     req.Index,
		Appended:  false,
		Delayed:   false,
		Failed:    true,
		Signature: []byte{},
	}
	//response.Signature = m.Server.Sign(ApproveAppendSignData(response))
	//peerId := req.Peer
	//if _, foundPeer := m.GroupPeers[peerId]; !foundPeer {
	//	log.Println("cannot approve append due to unexisted peer")
	//	return response, nil
	//}
	//response.Peer = m.Server.Id
	//if utils.VerifySign(m.Server.GetHostPublicKey(peerId), req.Signature, req.Hash) != nil {
	//	log.Println("cannot approve append due to signature verfication")
	//	return response, nil
	//}
	//lastIndex := m.LastEntryIndexNTXN()
	//if (lastIndex == req.Index && m.Leader == m.Server.Id) || lastIndex > req.Index {
	//	// this node will never have a chance to provide it's vote to the log
	//	// will check correctness and vote specifically for client peer without broadcasting
	//	m.Server.DB.View(func(txn *badger.Txn) error {
	//		entry := m.GetLogEntry(txn, req.Index)
	//		if entry != nil && bytes.Equal(entry.Hash, req.Hash) {
	//			response.Appended = true
	//			response.Failed = false
	//		}
	//		return nil
	//	})
	//} else {
	//	// this log entry have not yet been appended
	//	// in this case, this peer will just cast client peer vote
	//	// client peer should receive it's own vote from this peer by async
	//	groupPeers := m.OnboardGroupPeersSlice()
	//	m.PeerApprovedAppend(req.Index, peerId, groupPeers, true)
	//	response.Delayed = true
	//	response.Failed = false
	//}
	//response.Signature = s.Sign(ApproveAppendSignData(response))
	//log.Println(
	//	"approved append from", req.Peer, "for", req.Group, "to", req.Index,
	//	"failed:", response.Failed, "approved:", response.Appended, "delayed:", response.Delayed)
	return response, nil
}

func (m *RTGroup) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	// all of the leader transfer verification happens here
	group := m.Group
	groupId := m.Group.Id
	lastLogEntry := m.LastLogEntryNTXN()
	vote := &pb.RequestVoteResponse{
		Group:       groupId,
		Term:        group.GetTerm(),
		LogIndex:    lastLogEntry.Index,
		CandidateId: req.CandidateId,
		Voter:       m.Server.Id,
		Granted:     false,
		Signature:   []byte{},
	}
	log.Println("vote request from", req.CandidateId)
	if group == nil {
		log.Println("cannot grant vote to", req.CandidateId, ", cannot found group")
		return vote, nil
	}
	vote.Signature = m.Server.Sign(RequestVoteResponseSignData(vote))
	if req.Term-group.Term > utils.MAX_TERM_BUMP {
		// the candidate bump terms too fast
		log.Println("cannot grant vote to", req.CandidateId, ", term bump too fast")
		return vote, nil
	}
	if group.Term > req.Term || lastLogEntry.Index > req.LogIndex {
		// leader does not catch up
		log.Println("cannot grant vote to", req.CandidateId, ", candidate logs left behind")
		return vote, nil
	} else {
		m.ResetTerm(req.Term)
	}
	if m.VotedPeer != 0 {
		// already voted to other peer
		log.Println("cannot grant vote to", req.CandidateId, ", already voted to", m.VotedPeer)
		return vote, nil
	}
	if req.Term < group.Term {
		log.Println("cannot grant vote to", req.CandidateId, ", earlier term")
		return vote, nil
	}
	// TODO: check if the candidate really get the logs it claimed when the voter may fallen behind
	// Lazy voting
	// the condition for casting lazy voting is to wait until this peer turned into candidate
	// we also need to check it the peer candidate term is just what the request indicated
	waitedCounts := 0
	interval := 100
	secsToWait := 10
	intervalCount := secsToWait * 1000 / interval
	for true {
		<-time.After(time.Duration(interval) * time.Millisecond)
		waitedCounts++
		if m.Role == CANDIDATE && vote.Term == m.Group.Term {
			vote.Granted = true
			vote.Signature = m.Server.Sign(RequestVoteResponseSignData(vote))
			m.VotedPeer = req.CandidateId
			log.Println("grant vote to", req.CandidateId)
			break
		}
		if waitedCounts >= intervalCount {
			// timeout, will not grant
			log.Println("cannot grant vote to", req.CandidateId, ", time out")
			break
		}
	}
	return vote, nil
}

func (m *RTGroup) RPCGroupMembers(ctx context.Context, req *pb.GroupId) (*pb.GroupMembersResponse, error) {
	members := []*pb.GroupMember{}
	for _, p := range m.GroupPeers {
		host := m.Server.GetHostNTXN(p.Id)
		if host == nil {
			log.Println("cannot get host for group members")
			continue
		}
		members = append(members, &pb.GroupMember{
			Peer: p,
			Host: host,
		})
	}
	return &pb.GroupMembersResponse{
		Members:   members,
		Signature: m.Server.Sign(GetMembersSignData(members)),
		LastEntry: m.LastLogEntryNTXN(),
	}, nil
}
