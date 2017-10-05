package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/patrickmn/go-cache"
	"google.golang.org/grpc"
	"sync"
	"time"
)

type ClusterClient struct {
	conn *grpc.ClientConn
	rpc  pb.BFTRaftClient
}

type ClientStore struct {
	clients *cache.Cache
	lock    sync.Mutex
}

func (cs *ClientStore) Get(serverAddr string) (*ClusterClient, error) {
	if cachedClient, cachedFound := cs.clients.Get(serverAddr); cachedFound {
		return cachedClient.(*ClusterClient), nil
	}
	cs.lock.Lock()
	defer cs.lock.Unlock()
	if cachedClient, cachedFound := cs.clients.Get(serverAddr); cachedFound {
		return cachedClient.(*ClusterClient), nil
	}
	conn, err := grpc.Dial(serverAddr)
	if err != nil {
		return nil, err
	}
	rpcClient := pb.NewBFTRaftClient(conn)
	client := ClusterClient{conn, rpcClient}
	cs.clients.Set(serverAddr, client, cache.DefaultExpiration)
	return &client, nil
}

func NewClusterClientStore() ClientStore {
	store := ClientStore{
		clients: cache.New(10*time.Minute, 5*time.Minute),
	}
	store.clients.OnEvicted(func(host string, clientI interface{}) {
		client := clientI.(*ClusterClient)
		client.conn.Close()
	})
	return store
}
