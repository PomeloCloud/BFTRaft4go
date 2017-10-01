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
	"crypto/rsa"
)

type Options struct {
	max_replications uint32
	dbPath string
	address string
	privateKey []byte
}

type BFTRaftServer struct {
	Id uint64
	Opts Options
	DB *badger.KV
	Nodes map[uint64]*pb.Node
	FuncReg map[uint64]map[uint64]func(arg []byte) []byte
	Peers *cache.Cache
	Groups *cache.Cache
	PrivateKey *rsa.PrivateKey
	lock *sync.RWMutex
	clients ClientStore
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error)  {
	group_id := cmd.Group
	group := s.GetGroup(group_id)
	leader_peer_id := group.LeaderPeer
	leader_peer := s.GetPeer(group_id, leader_peer_id)
	response := &pb.CommandResponse{
		Group: cmd.Group,
		LeaderId: 0,
		NodeId: s.Id,
		RequestId: cmd.RequestId,
		Signature: s.Sign(U64Bytes(cmd.RequestId)),
		Result: []byte{},
	}
	if leader_node, found_leader := s.Nodes[leader_peer.Host]; found_leader {
		if leader_node.Id != s.Id {
			if client, err := s.clients.Get(leader_node.ServerAddr); err != nil {
				return client.rpc.ExecCommand(ctx, cmd)
			} else {
				return response, nil
			}
		} else {

		}
	} else {
		return response, nil
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
	config, err := GetConfig(db)
	if err != nil {
		return err
	}
	privateKey, err := ParsePrivateKey(config.PublicKey)
	if err != nil {
		return err
	}
	bftRaftServer := BFTRaftServer{
		Id: HashPublicKey(PublicKeyFromPrivate(privateKey)),
		Opts: serverOpts,
		DB: db,
		clients: NewClientStore(),
		Groups: cache.New(1 * time.Minute, 1 * time.Minute),
		Peers: cache.New(1 * time.Minute, 1 * time.Minute),
		PrivateKey: privateKey,
	}
	pb.RegisterBFTRaftServer(grpcServer, &bftRaftServer)
	bftRaftServer.LoadOnlineNodes()
	grpcServer.Serve(lis)
	return nil
}
