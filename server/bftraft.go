package server

import (
	"crypto/rsa"
	"flag"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"github.com/dgraph-io/badger"
	"github.com/patrickmn/go-cache"
	context "golang.org/x/net/context"
	"google.golang.org/grpc"
	"net"
	"sync"
	"time"
)

type Options struct {
	MaxReplications  uint32
	DBPath           string
	Address          string
	PrivateKey       []byte
	ConsensusTimeout time.Duration
}

type BFTRaftServer struct {
	Id                uint64
	Opts              Options
	DB                *badger.KV
	FuncReg           map[uint64]map[uint64]func(arg []byte) []byte
	Peers             *cache.Cache
	Groups            *cache.Cache
	GroupsPeers       *cache.Cache
	Nodes             *cache.Cache
	GroupAppendedLogs *cache.Cache
	GroupAppendLogRes *cache.Cache
	PrivateKey        *rsa.PrivateKey
	Clients           ClientStore
	lock              *sync.RWMutex
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error) {
	group_id := cmd.Group
	group := s.GetGroup(group_id)
	if group == nil {
		return nil, nil
	}
	leader_peer_id := group.LeaderPeer
	leader_peer := s.GetPeer(group_id, leader_peer_id)
	response := &pb.CommandResponse{
		Group:     cmd.Group,
		LeaderId:  0,
		NodeId:    s.Id,
		RequestId: cmd.RequestId,
		Signature: s.Sign(U64Bytes(cmd.RequestId)),
		Result:    []byte{},
	}
	if leader_peer == nil {
		return response, nil
	}
	if leader_node := s.GetNode(leader_peer.Host); leader_node != nil {
		if leader_node.Id != s.Id {
			if client, err := s.Clients.Get(leader_node.ServerAddr); err != nil {
				return client.rpc.ExecCommand(ctx, cmd)
			}
		} else { // the node is the leader to this group
			response.LeaderId = leader_peer.Id
			index := s.IncrGetGroupLogMaxIndex(group_id)
			hash, _ := LogHash(s.LastEntryHash(group_id), index)
			logEntry := pb.LogEntry{
				Term:    group.Term,
				Index:   index,
				Hash:    hash,
				Command: cmd,
			}
			if s.AppendEntryToLocal(group, logEntry) == nil {
				s.SendFollowersHeartbeat(ctx, group)
				response.Result = *s.WaitLogAppended(group_id, index)
			}
		}
	}
	return response, nil
}

func (s *BFTRaftServer) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.Nothing, error) {
	groupId := req.Group
	group := s.GetGroup(groupId)
	leaderId := req.LeaderId
	leaderPeer := s.GetPeer(groupId, leaderId)
	//response := *pb.AppendEntriesResponse{
	//	Group: groupId,
	//	Term: group.Term,
	//	Index: s.GetGroupLogMaxIndex(groupId),
	//	NodeId: s.Id,
	//	Successed: false,
	//	Convinced: true,
	//	Hash:
	//}
	nothing := &pb.Nothing{}
	// verify group and leader existence
	if group == nil || leaderPeer == nil {
		return nothing, nil
	}
	// check leader transfer
	if group.LeaderPeer != leaderId { // TODO: check for leader transfer
		return &pb.Nothing{}, nil
	}
	// check leader node exists
	leaderNode := s.GetNode(leaderPeer.Host)
	if leaderPeer == nil {
		return nothing, nil
	}
	// verify signature
	if leaderPublicKey, err := ParsePublicKey(leaderNode.PublicKey); err != nil {
		signData := AppendLogEntrySignData(group.Id, group.Term, req.PrevLogIndex, req.PrevLogTerm)
		if VerifySign(leaderPublicKey, req.Signature, signData) != nil {
			return nothing, nil
		}
	} else {
		return nothing, nil
	}
	// check log match
	if len(req.Entries) > 0 {
		iter := s.ReversedLogIterator(groupId)
		for true {
			log := iter.Next()
			if log == nil {
				break
			}
		}
	}
	return nil, nil
}

func (s *BFTRaftServer) RequestVote(context.Context, *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	return nil, nil
}

func (s *BFTRaftServer) RegisterServerFunc(group uint64, func_id uint64, fn func(arg []byte) []byte) {
	s.FuncReg[group][func_id] = fn
}

func (s *BFTRaftServer) SendFollowersHeartbeat(ctx context.Context, group *pb.RaftGroup) {
	group_peers := s.GetGroupPeers(group.Id)
	host_peers := map[*pb.Peer]bool{}
	for _, peer := range group_peers {
		host_peers[peer] = true
	}
	for peer := range host_peers {
		s.SendPeerUncommittedLogEntries(ctx, group, peer)
	}
}

func start(serverOpts Options) error {
	flag.Parse()
	lis, err := net.Listen("tcp", serverOpts.Address)
	if err != nil {
		return err
	}
	dbopt := badger.DefaultOptions
	dbopt.Dir = serverOpts.DBPath
	dbopt.ValueDir = serverOpts.DBPath
	db, err := badger.NewKV(&dbopt)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	config, err := GetConfig(db)
	if err != nil {
		return err
	}
	privateKey, err := ParsePrivateKey(config.PublicKey)
	if err != nil {
		return err
	}
	bftRaftServer := BFTRaftServer{
		Id:                HashPublicKey(PublicKeyFromPrivate(privateKey)),
		Opts:              serverOpts,
		DB:                db,
		Clients:           NewClientStore(),
		Groups:            cache.New(1*time.Minute, 1*time.Minute),
		GroupsPeers:       cache.New(1*time.Minute, 1*time.Minute),
		Peers:             cache.New(1*time.Minute, 1*time.Minute),
		Nodes:             cache.New(1*time.Minute, 1*time.Minute),
		GroupAppendLogRes: cache.New(2*time.Minute, 1*time.Minute),
		GroupAppendedLogs: cache.New(5*time.Minute, 1*time.Minute),
		PrivateKey:        privateKey,
	}
	pb.RegisterBFTRaftServer(grpcServer, &bftRaftServer)
	grpcServer.Serve(lis)
	return nil
}
