package utils

import (
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"sync"
	"google.golang.org/grpc"
	"net"
)

var RPCServers map[string]*grpc.Server = map[string]*grpc.Server{}
var RPCLock sync.Mutex = sync.Mutex{}

func GetClusterRPC(addr string) (spb.BFTRaftClient, error) {
	if cc, err := GetClientConn(addr); err == nil {
		return spb.NewBFTRaftClient(cc), nil
	} else {
		return nil, err
	}
}

func GetGRPCServer(addr string) *grpc.Server {
	RPCLock.Lock()
	defer RPCLock.Unlock()
	if cachedPRC, found := RPCServers[addr]; found {
		return cachedPRC
	} else {
		grpcServer := grpc.NewServer()
		RPCServers[addr] = grpcServer
		return grpcServer
	}
}

func GRPCServerListen(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return GetGRPCServer(addr).Serve(lis)
}