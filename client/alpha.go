package client

import (
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"log"
	"sync"
	"time"
)

type AlphaRPCsCache struct {
	lock       sync.Mutex
	bootstraps []string
	rpcs       []*spb.BFTRaftClient
	lastCheck  time.Time
}

func (arc *AlphaRPCsCache) ResetBootstrap(addrs []string) {
	arc.bootstraps = addrs
}

func (arc *AlphaRPCsCache) Get() []*spb.BFTRaftClient {
	arc.lock.Lock()
	defer arc.lock.Unlock()
	if len(arc.rpcs) < 1 || time.Now().After(arc.lastCheck.Add(5*time.Minute)) {
		nodes := utils.AlphaNodes(arc.bootstraps)
		arc.rpcs = []*spb.BFTRaftClient{}
		bootstraps := []string{}
		for _, node := range nodes {
			if c, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
				arc.rpcs = append(arc.rpcs, &c)
				bootstraps = append(bootstraps, node.ServerAddr)
			}
		}
		if len(bootstraps) > 0 {
			arc.ResetBootstrap(bootstraps)
		}
		arc.lastCheck = time.Now()
		log.Println("alpha nodes refreshed:", len(arc.rpcs))
	}
	return arc.rpcs
}

func NewAlphaRPCsCache(bootstraps []string) AlphaRPCsCache {
	return AlphaRPCsCache{
		lock:       sync.Mutex{},
		bootstraps: bootstraps,
		rpcs:       []*spb.BFTRaftClient{},
		lastCheck:  time.Now(),
	}
}
