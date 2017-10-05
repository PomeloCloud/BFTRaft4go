package client

import (
	"crypto/rsa"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	serverPb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)

type BFTRaftClient struct {
	rpc        *serverPb.BFTRaftClient
	feedback   *pb.BFTRaftClientServer
	privateKey *rsa.PrivateKey
}
