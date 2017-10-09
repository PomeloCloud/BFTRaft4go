package client

import (
	"context"
	"crypto/rsa"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"sync"
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
	bftclient.AlphaNodes = utils.AlphaNodes(bootstraps)
	return nil, nil
}
