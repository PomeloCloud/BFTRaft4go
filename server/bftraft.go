package server

import (
	context "golang.org/x/net/context"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"flag"
	"net"
	"google.golang.org/grpc"
	"github.com/dgraph-io/badger"
	"sync"
)

type Options struct {
	max_replications uint32
	dbPath string
	address string
}

type BFTRaftServer struct {
	Opts Options
	DB *badger.KV
	Nodes []*pb.Node
	FuncReg map[uint64]map[uint64]func(arg []byte) []byte
	Peers map[uint64][]pb.Peer
	lock *sync.RWMutex
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error)  {
	group := cmd.Group
	peers := s.GetGroupPeers(group)

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
	}
	pb.RegisterBFTRaftServer(grpcServer, &bftRaftServer)
	bftRaftServer.LoadOnlineNodes()
	grpcServer.Serve(lis)
	return nil
}
