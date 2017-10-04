package server

import (
	"fmt"
	"github.com/patrickmn/go-cache"
	"time"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
)

func (s *BFTRaftServer) WaitLogAppended(groupId uint64, logIndex uint64) *[]byte {
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	if _, existed := s.GroupAppendedLogs.Get(cache_key); !existed {
		s.GroupAppendedLogs.Set(cache_key, make(chan *[]byte, 1), cache.DefaultExpiration)
	}
	cache_chan, _ := s.GroupAppendedLogs.Get(cache_key)
	select {
		case bs:=  <- cache_chan.(chan *[]byte):
			return bs
		case <- time.After(s.Opts.ConsensusTimeout):
			return nil
	}
}

func (s *BFTRaftServer) SetLogAppended(groupId uint64, logIndex uint64) {
	cache_key := fmt.Sprint(groupId, "-", logIndex)
	if c, existed := s.GroupAppendedLogs.Get(cache_key); existed {
		c.(chan bool) <- true
	}
}

func ExpectedHonestPeers(group_peers []*pb.Peer) int {
	num_peers := len(group_peers)
	return num_peers - (num_peers - 1) / 3
}