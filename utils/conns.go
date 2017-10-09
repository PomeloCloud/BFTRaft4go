package utils

import (
	"google.golang.org/grpc"
	"sync"
)

var ClientConn map[string]*grpc.ClientConn = map[string]*grpc.ClientConn{}
var ConnLock sync.Mutex = sync.Mutex{}

func GetClientConn(addr string) (*grpc.ClientConn, error) {
	ConnLock.Lock()
	defer ConnLock.Unlock()
	if cachedConn, cacheFound := ClientConn[addr]; cacheFound {
		return cachedConn, nil
	}
	if conn, err := grpc.Dial(addr); err == nil {
		ClientConn[addr] = conn
		return conn, nil
	} else {
		return nil, err
	}
}
