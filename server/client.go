package server

import (
	"github.com/patrickmn/go-cache"
	"time"
	"google.golang.org/grpc"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"sync"
)

type Client struct {
	conn *grpc.ClientConn
	client pb.BFTRaftClient
}

type ClientStore struct {
	clients *cache.Cache
	lock sync.Mutex
}

func (cs *ClientStore)Get(serverAddr string) (*Client, error)  {
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
	rpcClient := pb.NewBFTRaftClient(conn)
	client := Client{conn, rpcClient}
	cs.clients.Set(serverAddr, client, cache.DefaultExpiration)
	return &client, nil
}

func NewClientStore() ClientStore {
	store := ClientStore{
		clients: cache.New(10 * time.Minute, 5 * time.Minute),
	}
	store.clients.OnEvicted(func(host string, clientI interface{}) {
		client := clientI.(*Client)
		client.conn.Close()
	})
	return store
}