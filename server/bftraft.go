package server

import (
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
	cmap "github.com/orcaman/concurrent-map"
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
	Id   uint64
	Opts Options
	DB   *badger.DB
	// first 10 is reserved for the alpha group
	FuncReg          map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte
	GroupsOnboard    cmap.ConcurrentMap
	GroupInvitations map[uint64]chan *pb.GroupInvitation
	PendingNewGroups map[uint64]chan error
	Groups           *cache.Cache
	Hosts            *cache.Cache
	NodePublicKeys   *cache.Cache
	ClientPublicKeys *cache.Cache
	Client           *client.BFTRaftClient
	PrivateKey       *rsa.PrivateKey
	ClientRPCs       ClientStore
	lock             sync.RWMutex
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
	m := s.GetOnboardGroup(cmd.Group)
	if m != nil && m.Leader == s.Id {
		m.Lock.Lock()
		defer m.Lock.Unlock()
		isRegNewNode := false
		log.Println("executing command group:", cmd.Group, "func:", cmd.FuncId, "client:", cmd.ClientId)
		if s.GetHostNTXN(cmd.ClientId) == nil && cmd.Group == utils.ALPHA_GROUP && cmd.FuncId == REG_NODE {
			// if registering new node, we should skip the signature verification
			log.Println("cannot find node and it's trying to register")
			isRegNewNode = true
		}
		if isRegNewNode || s.VerifyCommandSign(cmd) { // the node is the leader to this group
			response.LeaderId = s.Id
			var index uint64
			var hash []byte
			var logEntry pb.LogEntry
			if err := s.DB.Update(func(txn *badger.Txn) error {
				index = m.LastEntryIndex(txn) + 1
				hash, _ = utils.LogHash(m.LastEntryHash(txn), index, cmd.FuncId, cmd.Arg)
				logEntry = pb.LogEntry{
					Term:    m.Group.Term,
					Index:   index,
					Hash:    hash,
					Command: cmd,
				}
				return m.AppendEntryToLocal(txn, &logEntry)
			}); err == nil {
				m.SendFollowersHeartbeat(ctx)
				if len(m.GroupPeers) < 2 || m.WaitLogApproved(index) {
					response.Result = *m.CommitGroupLog(&logEntry)
				}
			} else {
				log.Println("append entry on leader failed:", err)
			}
		}
	} else {
		var host *pb.Host
		if m != nil {
			host = s.GetHostNTXN(m.Leader)
		} else {
			s.DB.View(func(txn *badger.Txn) error {
				peers := GetGroupPeersFromKV(txn, group_id)
				for id := range peers {
					host = s.GetHost(txn, id)
					break
				}
				return nil
			})
		}
		if c, err := utils.GetClusterRPC(host.ServerAddr); err == nil {
			return c.ExecCommand(ctx, cmd)
		}
	}
	response.Signature = s.Sign(utils.CommandSignData(
		response.Group, response.NodeId, response.RequestId, response.Result,
	))
	return response, nil
}

func (s *BFTRaftServer) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	groupId := req.Group
	groupMeta := s.GetOnboardGroup(groupId)
	if groupMeta == nil {
		errStr := fmt.Sprint("cannot append, group ", req.Group, " not on ", s.Id)
		log.Println(errStr)
		return nil, errors.New(errStr)
	}
	return groupMeta.AppendEntries(ctx, req)
}

func (s *BFTRaftServer) ApproveAppend(ctx context.Context, req *pb.AppendEntriesResponse) (*pb.ApproveAppendResponse, error) {
	groupMeta := s.GetOnboardGroup(req.Group)
	if groupMeta == nil {
		return nil, errors.New("cannot find the group")
	}
	return groupMeta.ApproveAppend(ctx, req)
}

func (s *BFTRaftServer) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	// all of the leader transfer verification happens here
	groupId := req.Group
	meta := s.GetOnboardGroup(groupId)
	if meta == nil {
		return nil, errors.New("cannot find the group")
	}
	return meta.RequestVote(ctx, req)
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

// this function should be called only on group members
func (s *BFTRaftServer) GroupMembers(ctx context.Context, req *pb.GroupId) (*pb.GroupMembersResponse, error) {
	meta := s.GetOnboardGroup(req.GroupId)
	if meta == nil {
		return nil, errors.New("cannot find group")
	}
	return meta.RPCGroupMembers(ctx, req)
}

func (s *BFTRaftServer) GetGroupContent(ctx context.Context, req *pb.GroupId) (*pb.RaftGroup, error) {
	group := s.GetGroupNTXN(req.GroupId)
	if group == nil {
		return nil, errors.New("cannot find group")
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

func (s *BFTRaftServer) RegisterRaftFunc(func_id uint64, fn func(arg *[]byte, entry *pb.LogEntry) []byte) {
	s.FuncReg[func_id] = fn
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
		Id:               id,
		Opts:             serverOpts,
		DB:               db,
		ClientRPCs:       NewClientStore(),
		Groups:           cache.New(1*time.Minute, 1*time.Minute),
		Hosts:            cache.New(1*time.Minute, 1*time.Minute),
		NodePublicKeys:   cache.New(5*time.Minute, 1*time.Minute),
		ClientPublicKeys: cache.New(5*time.Minute, 1*time.Minute),
		GroupInvitations: map[uint64]chan *pb.GroupInvitation{},
		GroupsOnboard:    cmap.New(),
		FuncReg:          map[uint64]func(arg *[]byte, entry *pb.LogEntry) []byte{},
		PendingNewGroups: map[uint64]chan error{},
		Client:           nclient,
		PrivateKey:       privateKey,
	}
	log.Println("scanning hosted groups")
	bftRaftServer.ScanHostedGroups(id)
	log.Println("registering membership contracts")
	bftRaftServer.RegisterMembershipCommands()
	log.Println("learning network node members")
	bftRaftServer.SyncAlphaGroup()
	log.Println("server generated:", bftRaftServer.Id)
	return &bftRaftServer, nil
}

func (s *BFTRaftServer) StartServer() error {
	log.Println("registering raft server service")
	pb.RegisterBFTRaftServer(utils.GetGRPCServer(s.Opts.Address), s)
	log.Println("registering raft feedback service")
	cpb.RegisterBFTRaftClientServer(utils.GetGRPCServer(s.Opts.Address), &client.FeedbackServer{ClientIns: s.Client})
	log.Println("going to start server with id:", s.Id, "on:", s.Opts.Address)
	go utils.GRPCServerListen(s.Opts.Address)
	time.Sleep(1 * time.Second)
	log.Println("registering this host")
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
		log.Println("generated wallet")
	} else {
		println("Cannot generate private key for the server")
	}
}
