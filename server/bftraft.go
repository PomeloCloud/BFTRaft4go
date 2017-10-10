package server

import (
	"bytes"
	"crypto/rsa"
	"flag"
	cpb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"golang.org/x/net/context"
	"sync"
	"time"
)

const (
	MAX_TERM_BUMP = 10
	ALPHA_GROUP   = 1 // the group for recording server members, groups, peers etc
)

type Options struct {
	DBPath           string
	Address          string
	bootstrap        []string
	ConsensusTimeout time.Duration
	replications     uint32
}

type BFTRaftServer struct {
	Id                uint64
	Opts              Options
	DB                *badger.KV
	FuncReg           map[uint64]map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte
	GroupsOnboard     map[uint64]*RTGroupMeta
	Peers             *cache.Cache
	Groups            *cache.Cache
	Nodes             *cache.Cache
	Clients           *cache.Cache
	GroupAppendedLogs *cache.Cache
	GroupApprovedLogs *cache.Cache
	NodePublicKeys    *cache.Cache
	ClientPublicKeys  *cache.Cache
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
		Signature: s.Sign(CommandSignData(group_id, s.Id, cmd.RequestId, []byte{})),
		Result:    []byte{},
	}
	group := s.GetGroup(group_id)
	if group == nil {
		return response, nil
	}
	leader_peer_id := group.LeaderPeer
	leader_peer := s.GetPeer(group_id, leader_peer_id)
	if leader_peer == nil {
		return response, nil
	}
	if leader_node := s.GetNode(leader_peer.Host); leader_node != nil {
		if leader_node.Id != s.Id {
			if client, err := utils.GetClusterRPC(leader_node.ServerAddr); err != nil {
				return client.ExecCommand(ctx, cmd)
			}
		} else if s.VerifyCommandSign(cmd) { // the node is the leader to this group
			groupMeta := s.GroupsOnboard[group_id]
			response.LeaderId = leader_peer.Id
			groupMeta.Lock.Lock()
			defer groupMeta.Lock.Unlock()
			index := s.IncrGetGroupLogLastIndex(group_id)
			hash, _ := LogHash(s.LastEntryHash(group_id), index, cmd.FuncId, cmd.Arg)
			logEntry := pb.LogEntry{
				Term:    group.Term,
				Index:   index,
				Hash:    hash,
				Command: cmd,
			}
			if s.AppendEntryToLocal(group, &logEntry) == nil {
				s.SendFollowersHeartbeat(ctx, leader_peer_id, group)
				if s.WaitLogApproved(group_id, index) {
					response.Result = *s.CommitGroupLog(group_id, &logEntry)
				}
			}
		}
	}
	response.Signature = s.Sign(CommandSignData(
		response.Group, response.NodeId, response.RequestId, response.Result,
	))
	return response, nil
}

func (s *BFTRaftServer) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	groupId := req.Group
	groupMeta := s.GroupsOnboard[groupId]
	group := groupMeta.Group
	reqLeaderId := req.LeaderId
	leaderPeer := groupMeta.GroupPeers[reqLeaderId]
	lastLogHash := s.LastEntryHash(groupId)
	thisPeer := s.GroupServerPeer(groupId)
	thisPeerId := uint64(0)
	if thisPeer != nil {
		thisPeerId = thisPeer.Id
	}
	response := &pb.AppendEntriesResponse{
		Group:     groupId,
		Term:      group.Term,
		Index:     s.GetGroupLogLastIndex(groupId),
		Successed: false,
		Convinced: false,
		Hash:      lastLogHash,
		Signature: s.Sign(lastLogHash),
		Peer:      thisPeerId,
	}
	groupMeta.Lock.Lock()
	defer groupMeta.Lock.Unlock()
	// verify group and leader existence
	if thisPeer == nil || group == nil || leaderPeer == nil {
		return response, nil
	}
	// check leader transfer
	if len(req.QuorumVotes) > 0 && req.LeaderId != group.LeaderPeer {
		if groupMeta.VotedPeer == 0 {
			if !s.BecomeFollower(groupMeta, req) {
				return response, nil
			}
		} else {
			return response, nil
		}
	} else if req.LeaderId != group.LeaderPeer {
		return response, nil
	}
	response.Convinced = true
	// check leader node exists
	leaderNode := s.GetNode(leaderPeer.Host)
	if leaderPeer == nil {
		return response, nil
	}
	// verify signature
	if leaderPublicKey := s.GetNodePublicKey(leaderNode.Id); leaderPublicKey != nil {
		signData := AppendLogEntrySignData(group.Id, group.Term, req.PrevLogIndex, req.PrevLogTerm)
		if VerifySign(leaderPublicKey, req.Signature, signData) != nil {
			return response, nil
		}
	} else {
		return response, nil
	}
	if len(req.Entries) > 0 {
		// check last log matches the first provided by the leader
		// this strategy assumes split brain will never happened (on internet)
		// the leader will always provide the entries no more than it needed
		// if the leader failed to provide the right first entries, the follower
		// 	 will not commit the log but response with current log index and term instead
		// the leader should response immediately for failed follower response
		lastLogEntry := s.LastLogEntry(groupId)
		lastLogIdx := s.GetGroupLogLastIndex(groupId)
		nextLogIdx := lastLogIdx + 1
		if lastLogEntry.Index != lastLogIdx {
			// this is unexpected, should be detected quickly
			panic("Log list unmatched with last log index")
		}
		if req.PrevLogIndex == lastLogIdx && req.Entries[0].Index == nextLogIdx { // index matched
			if req.PrevLogTerm != lastLogEntry.Term {
				// log mismatch, cannot preceded
				// what to do next will leave to the leader
				return response, nil
			} else {
				// first log matched
				// but we still need to check hash for upcoming logs
				expectedHash := s.LastEntryHash(groupId)
				for i := nextLogIdx; i < nextLogIdx+uint64(len(req.Entries)); i++ {
					entry := req.Entries[i-nextLogIdx]
					cmd := entry.Command
					expectedHash, _ = LogHash(expectedHash, i, cmd.FuncId, cmd.Arg)
					if entry.Index != i || !bytes.Equal(entry.Hash, expectedHash) || !s.VerifyCommandSign(entry.Command) {
						// not all entries match or cannot verified
						return response, nil
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
				groupPeers := s.OnboardGroupPeersSlice(groupId)
				for _, entry := range req.Entries {
					for _, peer := range groupPeers {
						if peer.Host == s.Id {
							continue
						}
						if node := s.GetNode(peer.Host); node != nil {
							if client, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
								response.Term = entry.Term
								response.Index = entry.Index
								response.Hash = entry.Hash
								response.Signature = s.Sign(entry.Hash)
								go func() {
									if approveRes, err := client.ApproveAppend(ctx, response); err != nil {
										if VerifySign(
											s.GetNodePublicKey(node.Id),
											approveRes.Signature,
											ApproveAppendSignData(approveRes),
										) == nil {
											if approveRes.Appended && !approveRes.Delayed && !approveRes.Failed {
												s.PeerApprovedAppend(groupId, entry.Index, peer.Id, groupPeers, true)
											}
										}
									}
								}()
							}
						}
					}
					if s.WaitLogApproved(groupId, entry.Index) {
						s.IncrGetGroupLogLastIndex(groupId)
						s.AppendEntryToLocal(group, entry)
					}
					result := s.CommitGroupLog(groupId, entry)
					client := s.GetClient(entry.Command.ClientId)
					if client != nil {
						if rpc, err := s.ClientRPCs.Get(client.Address); err == nil {
							nodeId := s.Id
							reqId := entry.Command.RequestId
							signData := CommandSignData(groupId, nodeId, reqId, *result)
							rpc.rpc.ResponseCommand(ctx, &cpb.CommandResult{
								Group:     groupId,
								NodeId:    nodeId,
								RequestId: reqId,
								Result:    *result,
								Signature: s.Sign(signData),
							})
						}
					}
				}
				response.Successed = true
			}
		}
	}
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
		return response, nil
	}
	peerId := req.Peer
	peer := s.GetPeer(groupId, peerId)
	if peer == nil {
		return response, nil
	}
	thisPeer := s.GroupServerPeer(groupId)
	if thisPeer == nil {
		return response, nil
	}
	response.Peer = thisPeer.Id
	if VerifySign(s.GetNodePublicKey(peer.Host), req.Signature, req.Hash) != nil {
		return response, nil
	}
	if s.GetGroupLogLastIndex(groupId) > req.Index {
		// this node will never have a chance to provide it's vote to the log
		// will check correctness and vote specifically for client peer without broadcasting
		entry := s.GetLogEntry(groupId, req.Index)
		if entry != nil && bytes.Equal(entry.Hash, req.Hash) {
			response.Appended = true
			response.Failed = false
		}
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
	return response, nil
}

func (s *BFTRaftServer) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	// all of the leader transfer verification happens here
	groupId := req.Group
	meta := s.GroupsOnboard[groupId]
	group := meta.Group
	lastLogEntry := s.LastLogEntry(groupId)
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
	if req.Term-group.Term > MAX_TERM_BUMP {
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
		meta.Lock.Lock()
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
		meta.Lock.Unlock()
	}
	return vote, nil
}

func NodesSignData(nodes []*pb.Node) []byte {
	signData := []byte{}
	for _, node := range nodes {
		nodeBytes, _ := proto.Marshal(node)
		signData = append(signData, nodeBytes...)
	}
	return signData
}

func GetPeersSignData(peers []*pb.Peer) []byte {
	signData := []byte{}
	for _, peer := range peers {
		peerBytes, _ := proto.Marshal(peer)
		signData = append(signData, peerBytes...)
	}
	return signData
}

func (s *BFTRaftServer) GroupNodes(ctx context.Context, request *pb.GroupId) (*pb.GroupNodesResponse, error) {
	// Outlet for group server memberships that contains all of the meta data on the network
	// This API is intended to be invoked from any machine to any members in the cluster
	nodes := []*pb.Node{}
	peers := GetGroupPeersFromKV(request.GroupId, s.DB)
	for _, peer := range peers {
		node := s.GetNode(peer.Host)
		nodes = append(nodes, node)
	}
	// signature should be optional for clients in case of the client don't know server public keys
	return &pb.GroupNodesResponse{Nodes: nodes, Signature: s.Sign(NodesSignData(nodes))}, nil
}

func (s *BFTRaftServer) GroupPeers(ctx context.Context, req *pb.GroupId) (*pb.GroupPeersResponse, error) {
	lastEntry := s.LastLogEntry(req.GroupId)
	peersMap := GetGroupPeersFromKV(req.GroupId, s.DB)
	peers := []*pb.Peer{}
	for _, p := range peersMap {
		peers = append(peers, p)
	}
	return &pb.GroupPeersResponse{
		Peers:     peers,
		Signature: s.Sign(GetPeersSignData(peers)),
		LastEntry: lastEntry,
	}, nil
}

func (s *BFTRaftServer) GetGroupContent(ctx context.Context, req *pb.GroupId) (*pb.RaftGroup, error) {
	return s.GetGroup(req.GroupId), nil
}

func (s *BFTRaftServer) PullGroupLogs(ctx context.Context, req *pb.PullGroupLogsResuest) (*pb.LogEntries, error) {
	keyPrefix := ComposeKeyPrefix(req.Group, LOG_ENTRIES)
	iter := s.DB.NewIterator(badger.IteratorOptions{})
	iter.Seek(append(keyPrefix, U64Bytes(uint64(req.Index))...))
	for true {

	}
	return nil, nil
}

func (s *BFTRaftServer) RegisterRaftFunc(group uint64, func_id uint64, fn func(arg *[]byte, entry *pb.LogEntry) []byte) {
	s.FuncReg[group][func_id] = fn
}

func (s *BFTRaftServer) SendFollowersHeartbeat(ctx context.Context, leader_peer_id uint64, group *pb.RaftGroup) {
	group_peers := s.GroupsOnboard[group.Id].GroupPeers
	host_peers := map[*pb.Peer]bool{}
	for _, peer := range group_peers {
		host_peers[peer] = true
	}
	for peer := range host_peers {
		if peer.Id != leader_peer_id {
			go func() {
				s.SendPeerUncommittedLogEntries(ctx, group, peer)
			}()
		}
	}
	RefreshTimer(s.GroupsOnboard[group.Id], 1)
}

func StartServer(serverOpts Options) error {
	flag.Parse()
	dbopt := badger.DefaultOptions
	dbopt.Dir = serverOpts.DBPath
	dbopt.ValueDir = serverOpts.DBPath
	db, err := badger.NewKV(&dbopt)
	if err != nil {
		return err
	}
	config, err := GetConfig(db)
	if err != nil {
		return err
	}
	privateKey, err := ParsePrivateKey(config.PrivateKey)
	if err != nil {
		return err
	}
	id := HashPublicKey(PublicKeyFromPrivate(privateKey))
	bftRaftServer := BFTRaftServer{
		Id:                id,
		Opts:              serverOpts,
		DB:                db,
		ClientRPCs:        NewClientStore(),
		Groups:            cache.New(1*time.Minute, 1*time.Minute),
		Peers:             cache.New(1*time.Minute, 1*time.Minute),
		Nodes:             cache.New(1*time.Minute, 1*time.Minute),
		Clients:           cache.New(1*time.Minute, 1*time.Minute),
		GroupApprovedLogs: cache.New(2*time.Minute, 1*time.Minute),
		GroupAppendedLogs: cache.New(5*time.Minute, 1*time.Minute),
		NodePublicKeys:    cache.New(5*time.Minute, 1*time.Minute),
		ClientPublicKeys:  cache.New(5*time.Minute, 1*time.Minute),
		PrivateKey:        privateKey,
	}
	bftRaftServer.GroupsOnboard = ScanHostedGroups(db, id)
	bftRaftServer.SyncAlphaGroup(serverOpts.bootstrap)
	bftRaftServer.StartTimingWheel()
	pb.RegisterBFTRaftServer(utils.GetGRPCServer(serverOpts.Address), &bftRaftServer)
	return utils.GRPCServerListen(serverOpts.Address)
}

func InitDatabase(dbPath string) {
	config := pb.ServerConfig{}
	if privateKey, _, err := GenerateKey(); err == nil {
		config.PrivateKey = privateKey
		dbopt := badger.DefaultOptions
		dbopt.Dir = dbPath
		dbopt.ValueDir = dbPath
		db, err := badger.NewKV(&dbopt)
		if err != nil {
			panic(err)
		}
		configBytes, err := proto.Marshal(&config)
		db.Set(ComposeKeyPrefix(CONFIG_GROUP, SERVER_CONF), configBytes, 0x00)
		if err := db.Close(); err != nil {
			panic(err)
		}
	} else {
		println("Cannot generate private key for the server")
	}
}
