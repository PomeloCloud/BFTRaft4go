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

// bootstraps is a list of server address believed to be the member of the network
// the list does not need to contain alpha nodes since all of the nodes on the network will get informed
func NewClient(bootstraps []string) (BFTRaftClient, error) {

}