package server

import (
	context "golang.org/x/net/context"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"flag"
	"net"
	"google.golang.org/grpc"
	"github.com/dgraph-io/badger"
)

type BFTRaftServer struct {
	DB *badger.KV
	Nodes []*pb.Node
}

func (s *BFTRaftServer) ExecCommand(context.Context, *pb.CommandRequest) (*pb.CommandResponse, error)  {
	return nil, nil
}

func (s *BFTRaftServer) RequestVote(context.Context, *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error)  {
	return nil, nil
}

func (s *BFTRaftServer) AppendEntries(context.Context, *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error)  {
	return nil, nil
}

func start(dbPath string, address string) error {
	flag.Parse()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	dbopt := badger.DefaultOptions
	dbopt.Dir = dbPath
	dbopt.ValueDir = dbPath
	db, err := badger.NewKV(&dbopt)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterBFTRaftServer(grpcServer, &BFTRaftServer{ db })
	grpcServer.Serve(lis)
	return nil
}
