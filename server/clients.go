package server

import (
	"crypto/rsa"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"google.golang.org/grpc"
	"strconv"
	"sync"
	"time"
	"log"
)

type Client struct {
	conn *grpc.ClientConn
	rpc  pb.BFTRaftClientClient
}

type ClientStore struct {
	clients *cache.Cache
	lock    sync.Mutex
}

func (cs *ClientStore) Get(serverAddr string) (*Client, error) {
	if cachedClient, cachedFound := cs.clients.Get(serverAddr); cachedFound {
		return cachedClient.(*Client), nil
	}
	cs.lock.Lock()
	defer cs.lock.Unlock()
	if cachedClient, cachedFound := cs.clients.Get(serverAddr); cachedFound {
		return cachedClient.(*Client), nil
	}
	conn, err := grpc.Dial(serverAddr)
	if err != nil {
		return nil, err
	}
	rpcClient := pb.NewBFTRaftClientClient(conn)
	client := Client{conn, rpcClient}
	cs.clients.Set(serverAddr, client, cache.DefaultExpiration)
	return &client, nil
}

func NewClientStore() ClientStore {
	store := ClientStore{
		clients: cache.New(10*time.Minute, 5*time.Minute),
	}
	store.clients.OnEvicted(func(host string, clientI interface{}) {
		client := clientI.(*Client)
		client.conn.Close()
	})
	return store
}

func ClientDBID (clientId uint64) []byte {
	return append(ComposeKeyPrefix(CLIENT_LIST_GROUP, CLIENT), U64Bytes(clientId)...)
}

func (s *BFTRaftServer) GetClient(clientId uint64) *spb.Client {
	cacheKey := strconv.Itoa(int(clientId))
	if cachedClient, found := s.Clients.Get(cacheKey); found {
		return cachedClient.(*spb.Client)
	}
	dbKey := ClientDBID(clientId)
	item := &badger.Item{}
	if err := s.DB.Update(func(txn *badger.Txn) error {
		 txn.Get(dbKey)
		 return nil
	}); err == nil {
		data := ItemValue(item)
		if data == nil {
			return nil
		} else {
			client := spb.Client{}
			proto.Unmarshal(*data, &client)
			s.Clients.Set(cacheKey, &client, cache.DefaultExpiration)
			return &client
		}
	} else {
		log.Println(err)
		return nil
	}
}

func (s *BFTRaftServer) SaveClient (client *spb.Client) error {
	dbKey := ClientDBID(client.Id)
	if data, err := proto.Marshal(client); err == nil {
		return s.DB.Update(func(txn *badger.Txn) error {
			txn.Set(dbKey, data, 0x00)
			return nil
		})
	} else {
		return nil
	}
}

func CommandSignData(group uint64, sender uint64, reqId uint64, data []byte) []byte {
	groupBytes := U64Bytes(group)
	senderBytes := U64Bytes(sender)
	reqIdBytes := U64Bytes(reqId)
	return append(append(append(groupBytes, senderBytes...), reqIdBytes...), data...)
}

func (s *BFTRaftServer) GetClientPublicKey(clientId uint64) *rsa.PublicKey {
	cacheKey := strconv.Itoa(int(clientId))
	if cachedKey, found := s.ClientPublicKeys.Get(cacheKey); found {
		return cachedKey.(*rsa.PublicKey)
	}
	client := s.GetClient(clientId)
	if key, err := ParsePublicKey(client.PrivateKey); err == nil {
		return key
	} else {
		return nil
	}
}

func (s *BFTRaftServer) VerifyCommandSign(cmd *spb.CommandRequest) bool {
	signData := CommandSignData(
		cmd.Group,
		cmd.ClientId,
		cmd.RequestId,
		append(U64Bytes(cmd.FuncId), cmd.Arg...),
	)
	publicKey := s.GetClientPublicKey(cmd.ClientId)
	if publicKey == nil {
		return false
	} else {
		return VerifySign(publicKey, cmd.Signature, signData) == nil
	}
}
