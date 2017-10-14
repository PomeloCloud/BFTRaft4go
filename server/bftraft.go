package server

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"flag"
	"fmt"
	"github.com/PomeloCloud/BFTRaft4go/client"
	cpb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"golang.org/x/net/context"
	"log"
	"sync"
	"time"
)

type Options struct {
	DBPath           string
	Address          string
	Bootstrap        []string
	ConsensusTimeout time.Duration
}

type BFTRaftServer struct {
	Id                uint64
	Opts              Options
	DB                *badger.DB
	FuncReg           map[uint64]map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte
	GroupsOnboard     map[uint64]*RTGroupMeta
	GroupInvitations  map[uint64]chan *pb.GroupInvitation
	PendingNewGroups  map[uint64]chan error
	Peers             *cache.Cache
	Groups            *cache.Cache
	Hosts             *cache.Cache
	GroupAppendedLogs *cache.Cache
	GroupApprovedLogs *cache.Cache
	NodePublicKeys    *cache.Cache
	ClientPublicKeys  *cache.Cache
	Client            *client.BFTRaftClient
	PrivateKey        *rsa.PrivateKey
	ClientRPCs        ClientStore
	lock              sync.RWMutex
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error) {
	group_id := cmd.Group
	response := &pb.CommandResponse{
		Group:     cmd.Group,
		LeaderId:  0,
		NodeId:    s.Id,
		RequestId: cmd.RequestId,
		Signature: s.Sign(utils.CommandSignData(group_id, s.Id, cmd.RequestId, []byte{})),
		Result:    []byte{},
	}
	group := s.GetGroupNTXN(group_id)
	if group == nil {
		log.Println("cannot find group:", group_id, "on exec logs")
		return response, nil
	}
	leader_node := s.GroupLeader(group_id).Node
	leader_peer := s.GetPeerNTXN(group_id, leader_node.Id)
	if leader_peer == nil {
		log.Println("cannot find leader peer for group:", group_id, "on exec logs")
		return response, nil
	}
	if leader_node := s.GetHostNTXN(leader_peer.Id); leader_node != nil {
		isRegNewNode := false
		log.Println("executing command group:", cmd.Group, "func:", cmd.FuncId, "client:", cmd.ClientId)
		if s.GetHostNTXN(cmd.ClientId) == nil && cmd.Group == utils.ALPHA_GROUP && cmd.FuncId == REG_NODE {
			// if registering new node, we should skip the signature verification
			log.Println("cannot find node and it's trying to register")
			isRegNewNode = true
		}
		if leader_node.Id != s.Id {
			if client, err := utils.GetClusterRPC(leader_node.ServerAddr); err == nil {
				return client.ExecCommand(ctx, cmd)
			}
		} else if isRegNewNode || s.VerifyCommandSign(cmd) { // the node is the leader to this group
			groupMeta := s.GroupsOnboard[group_id]
			groupMeta.Lock.Lock()
			defer groupMeta.Lock.Unlock()
			response.LeaderId = leader_peer.Id
			var index uint64
			var hash []byte
			var logEntry pb.LogEntry
			if err := s.DB.Update(func(txn *badger.Txn) error {
				index = s.LastEntryIndex(txn, group_id) + 1
				hash, _ = utils.LogHash(s.LastEntryHash(txn, group_id), index, cmd.FuncId, cmd.Arg)
				logEntry = pb.LogEntry{
					Term:    group.Term,
					Index:   index,
					Hash:    hash,
					Command: cmd,
				}
				return s.AppendEntryToLocal(txn, group, &logEntry)
			}); err == nil {
				s.SendFollowersHeartbeat(ctx, leader_peer.Id, group)
				if len(groupMeta.GroupPeers) < 2 || s.WaitLogApproved(group_id, index) {
					response.Result = *s.CommitGroupLog(group_id, &logEntry)
				}
			} else {
				log.Println("append entry on leader failed:", err)
			}
		}
	} else {
		log.Println("cannot get node to execute command to group leader")
	}
	response.Signature = s.Sign(utils.CommandSignData(
		response.Group, response.NodeId, response.RequestId, response.Result,
	))
	return response, nil
}

func (s *BFTRaftServer) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	groupId := req.Group
	groupMeta, onboard := s.GroupsOnboard[groupId]
	groupMeta.Lock.Lock()
	defer groupMeta.Lock.Unlock()
	if !onboard {
		errStr := fmt.Sprint("cannot append, group ", req.Group, " not on ", s.Id)
		log.Println(errStr)
		return nil, errors.New(errStr)
	}
	group := groupMeta.Group
	reqLeaderId := req.LeaderId
	leaderPeer := groupMeta.GroupPeers[reqLeaderId]
	lastLogHash := s.LastEntryHashNTXN(groupId)
	// log.Println("append log from", req.LeaderId, "to", s.Id, "entries:", len(req.Entries))
	response := &pb.AppendEntriesResponse{
		Group:     groupId,
		Term:      group.Term,
		Index:     s.LastEntryIndexNTXN(groupId),
		Successed: false,
		Convinced: false,
		Hash:      lastLogHash,
		Signature: s.Sign(lastLogHash),
		Peer:      s.Id,
	}
	// verify group and leader existence
	if group == nil || leaderPeer == nil {
		// log.Println("host or group not existed on append entries")
		return response, nil
	}
	// check leader transfer
	if len(req.QuorumVotes) > 0 && req.LeaderId != groupMeta.Leader {
		if !s.BecomeFollower(groupMeta, req) {
			log.Println("cannot become a follower when append entries due to votes")
			return response, nil
		}
	} else if req.LeaderId != groupMeta.Leader {
		log.Println("leader not matches when append entries")
		return response, nil
	}
	response.Convinced = true
	// check leader node exists
	leaderNode := s.GetHostNTXN(leaderPeer.Id)
	if leaderPeer == nil {
		log.Println("cannot get leader when append entries")
		return response, nil
	}
	// verify signature
	if leaderPublicKey := s.GetHostPublicKey(leaderNode.Id); leaderPublicKey != nil {
		signData := AppendLogEntrySignData(group.Id, group.Term, req.PrevLogIndex, req.PrevLogTerm)
		if err := utils.VerifySign(leaderPublicKey, req.Signature, signData); err != nil {
			// log.Println("leader signature not right when append entries:", err)
			// TODO: Fix signature verification, crypto/rsa: verification error
			// return response, nil
		}
	} else {
		log.Println("cannot get leader public key when append entries")
		return response, nil
	}
	groupMeta.Timeout = time.Now().Add(10 * time.Second)
	if len(req.Entries) > 0 {
		log.Println("appending new entries for:", groupId, "total", len(req.Entries))
		// check last log matches the first provided by the leader
		// this strategy assumes split brain will never happened (on internet)
		// the leader will always provide the entries no more than it needed
		// if the leader failed to provide the right first entries, the follower
		// 	 will not commit the log but response with current log index and term instead
		// the leader should response immediately for failed follower response
		lastLogIdx := s.LastEntryIndexNTXN(groupId)
		nextLogIdx := lastLogIdx + 1
		lastLog := s.LastLogEntryNTXN(groupId)
		lastLogTerm := uint64(0)
		if lastLog != nil {
			lastLogTerm = lastLog.Term
		}
		if req.PrevLogIndex == lastLogIdx && req.Entries[0].Index == nextLogIdx { // index matched
			if req.PrevLogTerm != lastLogTerm {
				// log mismatch, cannot preceded
				// what to do next will leave to the leader
				log.Println("cannot get leader public key when append entries", req.PrevLogTerm, lastLogTerm)
				return response, nil
			} else {
				// first log matched
				// but we still need to check hash for upcoming logs
				expectedHash := s.LastEntryHashNTXN(groupId)
				for i := nextLogIdx; i < nextLogIdx+uint64(len(req.Entries)); i++ {
					entry := req.Entries[i-nextLogIdx]
					cmd := entry.Command
					expectedHash, _ = utils.LogHash(expectedHash, i, cmd.FuncId, cmd.Arg)
					if entry.Index != i || !bytes.Equal(entry.Hash, expectedHash) {
						log.Println("log mismatch", entry.Index, "-", i, ",", entry.Hash, "-", expectedHash)
						return response, nil
					}
					if !s.VerifyCommandSign(entry.Command) {
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
					if s.WaitLogApproved(groupId, entry.Index) {
						s.DB.Update(func(txn *badger.Txn) error {
							s.AppendEntryToLocal(txn, group, entry)
							return nil
						})
					}
					clientId := entry.Command.ClientId
					result := s.CommitGroupLog(groupId, entry)
					client := s.GetHostNTXN(clientId)
					if client != nil {
						if rpc, err := s.ClientRPCs.Get(client.ServerAddr); err == nil {
							nodeId := s.Id
							reqId := entry.Command.RequestId
							signData := utils.CommandSignData(groupId, nodeId, reqId, *result)
							if _, err := rpc.rpc.ResponseCommand(ctx, &cpb.CommandResult{
								Group:     groupId,
								NodeId:    nodeId,
								RequestId: reqId,
								Result:    *result,
								Signature: s.Sign(signData),
							}); err != nil {
								log.Println("cannot response command to ", clientId, ":", err)
							}
						}
						response.Term = entry.Term
						response.Index = entry.Index
						response.Hash = entry.Hash
						response.Signature = s.Sign(entry.Hash)
					} else {
						log.Println("cannot get node", entry.Command.ClientId, "for response command")
					}
					log.Println("done appending log", entry.Index, "for group", groupId, "total", len(req.Entries))
				}
				response.Successed = true
			}
		} else {
			log.Println(
				s.Id, "log mismatch: prev index",
				req.PrevLogIndex, "-", lastLogIdx,
				"next index", req.Entries[0].Index, "-", nextLogIdx,
			)
		}
	}
	// log.Println("report back to leader index:", response.Index)
	return response, nil
}

func (s *BFTRaftServer) ApproveAppend(ctx context.Context, req *pb.AppendEntriesResponse) (*pb.ApproveAppendResponse, error) {
	groupId := req.Group
	response := &pb.ApproveAppendResponse{
		Group:     groupId,
		Peer:      0,
		Index:     req.Index,
		Appended:  false,
		Delayed:   false,
		Failed:    true,
		Signature: []byte{},
	}
	response.Signature = s.Sign(ApproveAppendSignData(response))
	_, found := s.GroupsOnboard[groupId]
	if !found {
		log.Println("cannot approve append due to unexisted group")
		return response, nil
	}
	peerId := req.Peer
	peer := s.GetPeerNTXN(groupId, peerId)
	if peer == nil {
		log.Println("cannot approve append due to unexisted peer")
		return response, nil
	}
	if _, found := s.GroupsOnboard[groupId]; !found {
		log.Println("cannot approve append, this peer is not in group")
		return response, nil
	}
	response.Peer = s.Id
	if utils.VerifySign(s.GetHostPublicKey(peer.Id), req.Signature, req.Hash) != nil {
		log.Println("cannot approve append due to signature verfication")
		return response, nil
	}
	meta := s.GroupsOnboard[groupId]
	lastIndex := s.LastEntryIndexNTXN(groupId)
	if (lastIndex == req.Index && meta.Leader == s.Id) || lastIndex > req.Index {
		// this node will never have a chance to provide it's vote to the log
		// will check correctness and vote specifically for client peer without broadcasting
		s.DB.View(func(txn *badger.Txn) error {
			entry := s.GetLogEntry(txn, groupId, req.Index)
			if entry != nil && bytes.Equal(entry.Hash, req.Hash) {
				response.Appended = true
				response.Failed = false
			}
			return nil
		})
	} else {
		// this log entry have not yet been appended
		// in this case, this peer will just cast client peer vote
		// client peer should receive it's own vote from this peer by async
		groupPeers := s.OnboardGroupPeersSlice(groupId)
		s.PeerApprovedAppend(groupId, req.Index, peerId, groupPeers, true)
		response.Delayed = true
		response.Failed = false
	}
	response.Signature = s.Sign(ApproveAppendSignData(response))
	log.Println(
		"approved append from", req.Peer, "for", req.Group, "to", req.Index,
		"failed:", response.Failed, "approved:", response.Appended, "delayed:", response.Delayed)
	return response, nil
}

func (s *BFTRaftServer) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	// all of the leader transfer verification happens here
	groupId := req.Group
	meta := s.GroupsOnboard[groupId]
	group := meta.Group
	lastLogEntry := s.LastLogEntryNTXN(groupId)
	vote := &pb.RequestVoteResponse{
		Group:       groupId,
		Term:        group.GetTerm(),
		LogIndex:    lastLogEntry.Index,
		CandidateId: req.CandidateId,
		Voter:       meta.Peer,
		Granted:     false,
		Signature:   []byte{},
	}
	if group == nil {
		return vote, nil
	}
	vote.Signature = s.Sign(RequestVoteResponseSignData(vote))
	if group.Term >= req.Term || lastLogEntry.Index > req.LogIndex {
		// leader does not catch up
		return vote, nil
	}
	if meta.VotedPeer != 0 {
		// already voted to other peer
		return vote, nil
	}
	if req.Term-group.Term > utils.MAX_TERM_BUMP {
		// the candidate bump terms too fast
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
		if meta.Role == CANDIDATE && vote.Term == meta.Group.Term {
			vote.Granted = true
			vote.Signature = s.Sign(RequestVoteResponseSignData(vote))
			meta.VotedPeer = req.CandidateId
			break
		}
		if waitedCounts >= intervalCount {
			// timeout, will not grant
			break
		}
	}
	return vote, nil
}

func GetMembersSignData(members []*pb.GroupMember) []byte {
	signData := []byte{}
	for _, member := range members {
		memberBytes, _ := proto.Marshal(member)
		signData = append(signData, memberBytes...)
	}
	return signData
}

func (s *BFTRaftServer) GroupHosts(ctx context.Context, request *pb.GroupId) (*pb.GroupNodesResponse, error) {
	// Outlet for group server memberships that contains all of the meta data on the network
	// This API is intended to be invoked from any machine to any members in the cluster
	result := s.GetGroupHostsNTXN(request.GroupId)
	// signature should be optional for clients in case of the client don't know server public keys
	signature := s.Sign(utils.NodesSignData(result))
	return &pb.GroupNodesResponse{Nodes: result, Signature: signature}, nil
}

func (s *BFTRaftServer) GroupMembers(ctx context.Context, req *pb.GroupId) (*pb.GroupMembersResponse, error) {
	peersMap := map[uint64]*pb.Peer{}
	lastEntry := &pb.LogEntry{}
	s.DB.View(func(txn *badger.Txn) error {
		lastEntry = s.LastLogEntry(txn, req.GroupId)
		peersMap = GetGroupPeersFromKV(txn, req.GroupId)
		return nil
	})
	members := []*pb.GroupMember{}
	for _, p := range peersMap {
		host := s.GetHostNTXN(p.Id)
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
		Signature: s.Sign(GetMembersSignData(members)),
		LastEntry: lastEntry,
	}, nil
}

func (s *BFTRaftServer) GetGroupContent(ctx context.Context, req *pb.GroupId) (*pb.RaftGroup, error) {
	group := s.GetGroupNTXN(req.GroupId)
	if group == nil {
		log.Println(s.Id, "cannot find group", req.GroupId)
	}
	return group, nil
}

// TODO: Signature
func (s *BFTRaftServer) PullGroupLogs(ctx context.Context, req *pb.PullGroupLogsResuest) (*pb.LogEntries, error) {
	keyPrefix := ComposeKeyPrefix(req.Group, LOG_ENTRIES)
	result := []*pb.LogEntry{}
	err := s.DB.View(func(txn *badger.Txn) error {
		iter := txn.NewIterator(badger.IteratorOptions{})
		iter.Seek(append(keyPrefix, utils.U64Bytes(uint64(req.Index))...))
		if iter.ValidForPrefix(keyPrefix) {
			firstEntry := LogEntryFromKVItem(iter.Item())
			if firstEntry.Index == req.Index {
				for true {
					iter.Next()
					if iter.ValidForPrefix(keyPrefix) {
						entry := LogEntryFromKVItem(iter.Item())
						result = append(result, entry)
					} else {
						break
					}
				}
			} else {
				log.Println("First entry not match")
			}
		} else {
			log.Println("Requesting non existed")
		}
		return nil
	})
	return &pb.LogEntries{Entries: result}, err
}

func (s *BFTRaftServer) RegisterRaftFunc(group uint64, func_id uint64, fn func(arg *[]byte, entry *pb.LogEntry) []byte) {
	if _, found := s.FuncReg[group]; !found {
		s.FuncReg[group] = map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte{}
	}
	s.FuncReg[group][func_id] = fn
}

func (s *BFTRaftServer) SendFollowersHeartbeat(ctx context.Context, leader_peer_id uint64, group *pb.RaftGroup) {
	meta := s.GroupsOnboard[group.Id]
	meta.Lock.Lock()
	defer meta.Lock.Unlock()
	if meta.IsBusy.IsSet() {
		return
	}
	meta.IsBusy.Set()
	defer meta.IsBusy.UnSet()
	group_peers := meta.GroupPeers
	host_peers := map[*pb.Peer]bool{}
	for _, peer := range group_peers {
		host_peers[peer] = true
	}
	num_peers := len(host_peers)
	completion := make(chan *pb.AppendEntriesResponse, num_peers)
	sentMsgs := 0
	uncommittedEntries := map[uint64][]*pb.LogEntry{}
	peerPrevEntry := map[uint64]*pb.LogEntry{}
	for peer := range host_peers {
		if peer.Id != leader_peer_id {
			entries, prevEntry := s.PeerUncommittedLogEntries(group, peer)
			uncommittedEntries[peer.Id] = entries
			peerPrevEntry[peer.Id] = prevEntry
			node := s.GetHostNTXN(peer.Id)
			if node == nil {
				log.Println("cannot get node for send peer uncommitted log entries")
				completion <- nil
				return
			}
			votes := []*pb.RequestVoteResponse{}
			if meta.SendVotesForPeers[meta.Peer] {
				votes = meta.Votes
			}
			signData := AppendLogEntrySignData(group.Id, group.Term, prevEntry.Index, prevEntry.Term)
			signature := s.Sign(signData)
			sentMsgs++
			go func() {
				if client, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
					if appendResult, err := client.AppendEntries(ctx, &pb.AppendEntriesRequest{
						Group:        group.Id,
						Term:         group.Term,
						LeaderId:     s.Id,
						PrevLogIndex: prevEntry.Index,
						PrevLogTerm:  prevEntry.Term,
						Signature:    signature,
						QuorumVotes:  votes,
						Entries:      entries,
					}); err == nil {
						appendResult.Peer = peer.Id
						completion <- appendResult
					} else {
						log.Println("append log failed:", err)
						completion <- nil
					}
				}
			}()

		}
	}
	// log.Println("sending log to", sentMsgs, "followers with", num_peers, "peers")
	for i := 0; i < sentMsgs; i++ {
		response := <-completion
		if response == nil {
			continue
		}
		peerId := response.Peer
		peer := meta.GroupPeers[peerId]
		publicKey := s.GetHostPublicKey(peerId)
		if publicKey == nil {
			log.Println("cannot find public key for:", peerId)
			continue
		}
		if utils.VerifySign(publicKey, response.Signature, response.Hash) != nil {
			return
		}
		var lastEntry *pb.LogEntry
		entries := uncommittedEntries[peerId]
		if len(entries) == 0 {
			lastEntry = peerPrevEntry[peerId]
		} else {
			lastEntry = entries[len(entries)-1]
		}
		if lastEntry == nil {
			log.Println("cannot found lastEntry")
			continue
		}
		if response.Index != peer.MatchIndex {
			peer.MatchIndex = response.Index
			peer.NextIndex = peer.MatchIndex + 1
			meta.GroupPeers[peer.Id] = peer
			if err := s.DB.Update(func(txn *badger.Txn) error {
				return s.SavePeer(txn, peer)
			}); err != nil {
				log.Println("cannot save peer:", peer.Id, err)
			}
		}
		meta.SendVotesForPeers[meta.Peer] = !response.Convinced
	}
}

func (s *BFTRaftServer) GetGroupLeader(ctx context.Context, req *pb.GroupId) (*pb.GroupLeader, error) {
	return s.GroupLeader(req.GroupId), nil
}

func (s *BFTRaftServer) SendGroupInvitation(ctx context.Context, inv *pb.GroupInvitation) (*pb.Nothing, error) {
	// TODO: verify invitation signature
	go func() {
		s.GroupInvitations[inv.Group] <- inv
	}()
	return &pb.Nothing{}, nil
}

func (s *BFTRaftServer) Sign(data []byte) []byte {
	return utils.Sign(s.PrivateKey, data)
}

func GetServer(serverOpts Options) (*BFTRaftServer, error) {
	flag.Parse()
	dbopt := badger.DefaultOptions
	dbopt.Dir = serverOpts.DBPath
	dbopt.ValueDir = serverOpts.DBPath
	db, err := badger.Open(&dbopt)
	if err != nil {
		log.Panic(err)
		return nil, err
	}
	config, err := GetConfig(db)
	if err != nil {
		log.Panic(err)
		return nil, err
	}
	privateKey, err := utils.ParsePrivateKey(config.PrivateKey)
	if err != nil {
		log.Panic("error on parse key", config.PrivateKey, err)
		return nil, err
	}
	id := utils.HashPublicKey(utils.PublicKeyFromPrivate(privateKey))
	nclient, err := client.NewClient(serverOpts.Bootstrap, client.ClientOptions{PrivateKey: config.PrivateKey})
	if err != nil {
		log.Panic(err)
		return nil, err
	}
	bftRaftServer := BFTRaftServer{
		Id:                id,
		Opts:              serverOpts,
		DB:                db,
		ClientRPCs:        NewClientStore(),
		Groups:            cache.New(1*time.Minute, 1*time.Minute),
		Peers:             cache.New(1*time.Minute, 1*time.Minute),
		Hosts:             cache.New(1*time.Minute, 1*time.Minute),
		GroupApprovedLogs: cache.New(2*time.Minute, 1*time.Minute),
		GroupAppendedLogs: cache.New(5*time.Minute, 1*time.Minute),
		NodePublicKeys:    cache.New(5*time.Minute, 1*time.Minute),
		ClientPublicKeys:  cache.New(5*time.Minute, 1*time.Minute),
		GroupInvitations:  map[uint64]chan *pb.GroupInvitation{},
		FuncReg:           map[uint64]map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte{},
		Client:            nclient,
		PrivateKey:        privateKey,
	}
	bftRaftServer.GroupsOnboard = ScanHostedGroups(db, id)
	bftRaftServer.RegisterMembershipCommands()
	bftRaftServer.SyncAlphaGroup()
	log.Println("server generated:", bftRaftServer.Id)
	return &bftRaftServer, nil
}

func (s *BFTRaftServer) StartServer() error {
	s.StartTimingWheel()
	pb.RegisterBFTRaftServer(utils.GetGRPCServer(s.Opts.Address), s)
	cpb.RegisterBFTRaftClientServer(utils.GetGRPCServer(s.Opts.Address), &client.FeedbackServer{ClientIns: s.Client})
	log.Println("going to start server with id:", s.Id, "on:", s.Opts.Address)
	go utils.GRPCServerListen(s.Opts.Address)
	time.Sleep(1 * time.Second)
	s.RegHost()
	return nil
}

func InitDatabase(dbPath string) {
	config := pb.ServerConfig{}
	if privateKey, _, err := utils.GenerateKey(); err == nil {
		config.PrivateKey = privateKey
		dbopt := badger.DefaultOptions
		dbopt.Dir = dbPath
		dbopt.ValueDir = dbPath
		db, err := badger.Open(&dbopt)
		if err != nil {
			panic(err)
		}
		configBytes, err := proto.Marshal(&config)
		db.Update(func(txn *badger.Txn) error {
			return txn.Set(ComposeKeyPrefix(CONFIG_GROUP, SERVER_CONF), configBytes, 0x00)
		})
		if err := db.Close(); err != nil {
			panic(err)
		}
	} else {
		println("Cannot generate private key for the server")
	}
}
