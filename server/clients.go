package server

import (
	"sync"
	"github.com/patrickmn/go-cache"
	"google.golang.org/grpc"
	cpb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	"time"
)

type ClientStore struct {
	clients *cache.Cache
	lock    sync.Mutex
}

type Client struct {
	conn *grpc.ClientConn
	rpc  cpb.BFTRaftClientClient
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
	conn, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	rpcClient := cpb.NewBFTRaftClientClient(conn)
	client := Client{conn, rpcClient}
	cs.clients.Set(serverAddr, &client, cache.DefaultExpiration)
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