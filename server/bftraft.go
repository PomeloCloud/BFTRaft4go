package server

import (
	context "golang.org/x/net/context"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"flag"
	"net"
	"google.golang.org/grpc"
	"github.com/dgraph-io/badger"
	"github.com/patrickmn/go-cache"
	"time"
	"sync"
)

type Options struct {
	max_replications uint32
	dbPath string
	address string
	private_key []byte
}

type BFTRaftServer struct {
	Opts Options
	DB *badger.KV
	Nodes map[uint64]*pb.Node
	FuncReg map[uint64]map[uint64]func(arg []byte) []byte
	Peers *cache.Cache
	Groups *cache.Cache
	lock *sync.RWMutex
	clients ClientStore
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error)  {
	group_id := cmd.Group
	group := s.GetGroup(group_id)
	leader_peer_id := group.LeaderPeer
	leader_peer := s.GetPeer(group_id, leader_peer_id)
	if leader_node, found_leader := s.Nodes[leader_peer.Host]; found_leader {
		if client, err := s.clients.Get(leader_node.ServerAddr); err != nil {
			return client.rpc.ExecCommand(ctx, cmd)
		}
	} else {
		// return &pb.CommandResponse{}
	}
	return nil, nil
}

func (s *BFTRaftServer) RequestVote(context.Context, *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error)  {
	return nil, nil
}

func (s *BFTRaftServer) AppendEntries(context.Context, *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error)  {
	return nil, nil
}

func (s *BFTRaftServer) RegisterServerFunc(group uint64, func_id uint64, fn func(arg []byte)[]byte) {
	s.FuncReg[group][func_id] = fn
}

func start(serverOpts Options) error {
	flag.Parse()
	lis, err := net.Listen("tcp", serverOpts.address)
	if err != nil {
		return err
	}
	dbopt := badger.DefaultOptions
	dbopt.Dir = serverOpts.dbPath
	dbopt.ValueDir = serverOpts.dbPath
	db, err := badger.NewKV(&dbopt)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	bftRaftServer := BFTRaftServer{
		Opts: serverOpts,
		DB: db,
		clients: NewClientStore(),
		Groups: cache.New(1 * time.Minute, 1 * time.Minute),
		Peers: cache.New(1 * time.Minute, 1 * time.Minute),
	}
	pb.RegisterBFTRaftServer(grpcServer, &bftRaftServer)
	bftRaftServer.LoadOnlineNodes()
	grpcServer.Serve(lis)
	return nil
}
