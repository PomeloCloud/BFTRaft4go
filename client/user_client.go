package client

import (
	"context"
	"crypto/rsa"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/server"
	"sync"
	"github.com/PomeloCloud/BFTRaft4go/utils"
)

type BFTRaftClient struct {
	Id         uint64
	PrivateKey *rsa.PrivateKey
	AlphaNodes []*spb.Node
	Lock       sync.RWMutex
}

type ClientOptions struct {
	PrivateKey []byte
}

// bootstraps is a list of server address believed to be the member of the network
// the list does not need to contain alpha nodes since all of the nodes on the network will get informed
func NewClient(bootstraps []string, opts ClientOptions) (*BFTRaftClient, error) {
	privateKey, err := server.ParsePrivateKey(opts.PrivateKey)
	if err != nil {
		return nil, err
	}
	publicKey := server.PublicKeyFromPrivate(privateKey)
	bftclient := &BFTRaftClient{
		Id:         server.HashPublicKey(publicKey),
		PrivateKey: privateKey,
		Lock:       sync.RWMutex{},
	}
	bootstrapServers := []spb.BFTRaftClient{}
	for _, addr := range bootstraps {
		if c, err := utils.GetClusterRPC(addr); err == nil {
			bootstrapServers = append(bootstrapServers, c)
		}
	}
	alphaNodes := MajorityResponse(bootstrapServers, func(c spb.BFTRaftClient) (interface{}, []byte) {
		if nodes, err := c.AlphaNodes(context.Background(), &spb.Nothing{}); err == nil {
			return nodes, server.NodesSignData(nodes.Nodes)
		} else {
			return nil, []byte{}
		}
	}).(*spb.AlphaNodesResponse)
	bftclient.AlphaNodes = alphaNodes.Nodes
	return nil, nil
}
