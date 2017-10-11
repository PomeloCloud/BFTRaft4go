package client

import (
	"crypto/rsa"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/patrickmn/go-cache"
	"sync"
	"context"
)

type BFTRaftClient struct {
	Id         uint64
	PrivateKey *rsa.PrivateKey
	AlphaNodes []*spb.BFTRaftClient
	Groups     *cache.Cache
	CmdResChan map[uint64]map[uint64]chan []byte
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
		AlphaNodes: []*spb.BFTRaftClient{},
	}
	bftclient.RefreshAlphaNodes(bootstraps)
	return bftclient, nil
}

func (brc *BFTRaftClient) RefreshAlphaNodes(bootstraps []string) {
	nodes := utils.AlphaNodes(bootstraps)
	for _, node := range nodes {
		if c, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			brc.AlphaNodes = append(brc.AlphaNodes, &c)
		}
	}
}

func (brc *BFTRaftClient) GetGroupHosts(groupId uint64) *[]*spb.Host {
	return utils.MajorityResponse(brc.AlphaNodes, func(client spb.BFTRaftClient) (interface{}, []byte) {
		if res, err := client.GroupHosts(context.Background(), &spb.GroupId{GroupId: groupId}); err == nil {
			return &res.Nodes, utils.NodesSignData(res.Nodes)
		} else {
			return nil, []byte{}
		}
	}).(*[]*spb.Host)
}
