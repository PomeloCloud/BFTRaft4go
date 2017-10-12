package client

import (
	"context"
	"crypto/rsa"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/patrickmn/go-cache"
	"log"
)

type BFTRaftClient struct {
	Id          uint64
	PrivateKey  *rsa.PrivateKey
	AlphaRPCs   []*spb.BFTRaftClient
	GroupHosts  *cache.Cache
	GroupLeader *cache.Cache
	CmdResChan  map[uint64]map[uint64]chan []byte
	Counter     int64
	Lock        sync.RWMutex
}

type ClientOptions struct {
	PrivateKey []byte
}

// bootstraps is a list of server address believed to be the member of the network
// the list does not need to contain alpha nodes since all of the nodes on the network will get informed
func NewClient(bootstraps []string, opts ClientOptions) (*BFTRaftClient, error) {
	privateKey, err := utils.ParsePrivateKey(opts.PrivateKey)
	if err != nil {
		return nil, err
	}
	publicKey := utils.PublicKeyFromPrivate(privateKey)
	bftclient := &BFTRaftClient{
		Id:          utils.HashPublicKey(publicKey),
		PrivateKey:  privateKey,
		Lock:        sync.RWMutex{},
		AlphaRPCs:   []*spb.BFTRaftClient{},
		GroupHosts:  cache.New(1*time.Minute, 1*time.Minute),
		GroupLeader: cache.New(1*time.Minute, 1*time.Minute),
		Counter:     0,
	}
	bftclient.RefreshAlphaNodes(bootstraps)
	return bftclient, nil
}

func (brc *BFTRaftClient) RefreshAlphaNodes(bootstraps []string) {
	nodes := utils.AlphaNodes(bootstraps)
	log.Println("alpha nodes refreshed:", nodes)
	for _, node := range nodes {
		if c, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			brc.AlphaRPCs = append(brc.AlphaRPCs, &c)
		}
	}
}

func (brc *BFTRaftClient) GetGroupHosts(groupId uint64) *[]*spb.Host {
	cacheKey := strconv.Itoa(int(groupId))
	if cached, found := brc.GroupHosts.Get(cacheKey); found {
		return cached.(*[]*spb.Host)
	}
	res := utils.MajorityResponse(brc.AlphaRPCs, func(client spb.BFTRaftClient) (interface{}, []byte) {
		if res, err := client.GroupHosts(
			context.Background(), &spb.GroupId{GroupId: groupId},
		); err == nil {
			return &res.Nodes, utils.NodesSignData(res.Nodes)
		} else {
			return nil, []byte{}
		}
	})
	var hosts *[]*spb.Host = nil
	if res != nil {
		hosts = res.(*[]*spb.Host)
	}
	if hosts != nil {
		brc.GroupHosts.Set(cacheKey, hosts, cache.DefaultExpiration)
	}
	return hosts
}

func (brc *BFTRaftClient) GetGroupLeader(groupId uint64) spb.BFTRaftClient {
	cacheKey := strconv.Itoa(int(groupId))
	if cached, found := brc.GroupLeader.Get(cacheKey); found {
		return cached.(spb.BFTRaftClient)
	}
	leaderHost := utils.MajorityResponse(brc.AlphaRPCs, func(client spb.BFTRaftClient) (interface{}, []byte) {
		if res, err := client.GetGroupLeader(
			context.Background(), &spb.GroupId{GroupId: groupId},
		); err == nil {
			// TODO: verify signature
			return &res.Node, []byte(res.Node.ServerAddr)
		} else {
			return nil, []byte{}
		}
	}).(*spb.Host)
	if leaderHost != nil {
		if leader, err := utils.GetClusterRPC(leaderHost.ServerAddr); err == nil {
			brc.GroupHosts.Set(cacheKey, leader, cache.DefaultExpiration)
			return leader
		}
	}
	return nil
}

func (brc *BFTRaftClient) ExecCommand(groupId uint64, funcId uint64, arg []byte) (*[]byte, error) {
	leader := brc.GetGroupLeader(groupId)
	if leader == nil {
		return nil, errors.New("cannot found leader")
	}
	reqId := uint64(atomic.AddInt64(&brc.Counter, 1))
	cmdReq := &spb.CommandRequest{
		Group:     groupId,
		ClientId:  brc.Id,
		RequestId: reqId,
		FuncId:    funcId,
		Arg:       arg,
	}
	signData := utils.ExecCommandSignData(cmdReq)
	cmdReq.Signature = utils.Sign(brc.PrivateKey, signData)
	brc.CmdResChan[groupId][reqId] = make(chan []byte)
	defer func() {
		close(brc.CmdResChan[groupId][reqId])
		delete(brc.CmdResChan[groupId], reqId)
	}()
	if cmdRes, err := leader.ExecCommand(context.Background(), cmdReq); err == nil {
		// TODO: verify signature
		// TODO: update leader if needed
		// TODO: verify response matches request
		brc.CmdResChan[groupId][reqId] <- cmdRes.Result
	}
	expectedResponse := len(*brc.GetGroupHosts(groupId)) / 2
	responseReceived := map[uint64][]byte{}
	responseHashes := []uint64{}
	replicationCompleted := make(chan bool, 1)
	go func() {
		for res := range brc.CmdResChan[groupId][reqId] {
			hash := utils.HashData(res)
			responseReceived[hash] = res
			responseHashes = append(responseHashes, hash)
			if len(responseReceived) > expectedResponse {
				replicationCompleted <- true
				break
			}
		}
	}()
	select {
	case <-replicationCompleted:
		majorityHash := utils.PickMajority(responseHashes)
		majorityData := responseReceived[majorityHash]
		return &majorityData, nil
	case <-time.After(10 * time.Second):
		return nil, errors.New("does not receive enough response")
	}
}
