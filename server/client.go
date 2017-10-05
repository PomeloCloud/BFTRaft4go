package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	"github.com/patrickmn/go-cache"
	"google.golang.org/grpc"
	"sync"
	"time"
)

type Client struct {
	conn *grpc.ClientConn
	rpc pb.BFTRaftClientClient
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
		client := clientI.(*ClusterClient)
		client.conn.Close()
	})
	return store
}