package client

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	serverPb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"crypto/rsa"
)

type BFTRaftClient struct {
	rpc *serverPb.BFTRaftClient
	feedback *pb.BFTRaftClientServer
	privateKey *rsa.PrivateKey
}

