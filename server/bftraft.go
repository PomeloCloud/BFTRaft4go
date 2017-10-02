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
	FuncReg map[uint64]map[uint64]func(arg []byte) []byte
	Peers *cache.Cache
	Groups *cache.Cache
	GroupsPeers *cache.Cache
	Nodes *cache.Cache
	PrivateKey *rsa.PrivateKey
	lock *sync.RWMutex
	clients ClientStore
}

func (s *BFTRaftServer) ExecCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error)  {
	group_id := cmd.Group
	group := s.GetGroup(group_id)
	if group == nil {
		return nil, nil
	}
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
	if leader_peer == nil {
		return response, nil
	}
	if leader_node := s.GetNode(leader_peer.Host); leader_node != nil {
		if leader_node.Id != s.Id {
			if client, err := s.clients.Get(leader_node.ServerAddr); err != nil {
				return client.rpc.ExecCommand(ctx, cmd)
			} else {
				return response, nil
			}
		} else { // the node is the leader to this group
			hash, _ := SHA1Hash(s.LastEntryHash(group_id))
			logEntry := pb.LogEntry{
				Term: group.Term,
				Index: s.IncrGetGroupLogMaxIndex(group_id),
				Hash: hash,
				Command: cmd,
			}
			if s.AppendEntryToLocal(group, logEntry) == nil {
				s.SendFollowersHeartbeat(group_id)
			}
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

func (s *BFTRaftServer) SendFollowersHeartbeat(group_id uint64)  {
	group_peers := s.GetGroupPeers(group_id)
	host_peers := map[*pb.Peer]bool{}
	for _, peer := range group_peers {
		host_peers[peer] = true
	}
	for peer := range host_peers {
		node := s.GetNode(peer.Host)
		if node == nil {
			continue
		}
		if client, err := s.clients.Get(node.ServerAddr); err != nil {
			go func() {
				client.rpc.AppendEntries(&pb.AppendEntriesRequest{
					Group: ,
					Term: group.Term,
					LeaderId: s.Id,
					PrevLogIndex:
				})
			}()
		}
	}
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
		GroupsPeers: cache.New(1 * time.Minute, 1 * time.Minute),
		Peers: cache.New(1 * time.Minute, 1 * time.Minute),
		Nodes: cache.New(1 * time.Minute, 1 * time.Minute),
		PrivateKey: privateKey,
	}
	pb.RegisterBFTRaftServer(grpcServer, &bftRaftServer)
	grpcServer.Serve(lis)
	return nil
}
