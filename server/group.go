package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
	"sync"
	"time"
)

const (
	LEADER    = 0
	FOLLOWER  = 1
	CANDIDATE = 2
	OBSERVER  = 3
)

type RTGroupMeta struct {
	Peer            uint64
	Leader          uint64
	VotedPeer       uint64
	Lock            sync.RWMutex
	GroupPeers      map[uint64]*pb.Peer
	Group           *pb.RaftGroup
	Timeout         time.Time
	Role            int
	Votes           []*pb.RequestVoteResponse
	VotesForEntries map[uint64]bool // key is peer id
}

func GetGroupFromKV(groupId uint64, KV *badger.KV) *pb.RaftGroup {
	group := &pb.RaftGroup{}
	keyPrefix := ComposeKeyPrefix(groupId, GROUP_META)
	item := badger.KVItem{}
	KV.Get(keyPrefix, &item)
	data := ItemValue(&item)
	if data == nil {
		return nil
	} else {
		proto.Unmarshal(*data, group)
		return group
	}
}

func (s *BFTRaftServer) GetGroup(groupId uint64) *pb.RaftGroup {
	cacheKey := strconv.Itoa(int(groupId))
	cachedGroup, cacheFound := s.Groups.Get(cacheKey)
	if cacheFound {
		return cachedGroup.(*pb.RaftGroup)
	} else {
		group := GetGroupFromKV(groupId, s.DB)
		if group != nil {
			s.Groups.Set(cacheKey, group, cache.DefaultExpiration)
			return group
		} else {
			return nil
		}
	}
}

func (s *BFTRaftServer) GetGroupLogLastIndex(groupId uint64) uint64 {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	item := badger.KVItem{}
	s.DB.Get(key, &item)
	data := ItemValue(&item)
	var idx uint64 = 0
	if data != nil {
		idx = BytesU64(*data, 0)
	}
	return idx
}

func (s *BFTRaftServer) IncrGetGroupLogLastIndex(groupId uint64) uint64 {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	for true {
		item := badger.KVItem{}
		s.DB.Get(key, &item)
		data := ItemValue(&item)
		var idx uint64 = 0
		if data != nil {
			idx = BytesU64(*data, 0)
		}
		idx += 1
		if s.DB.CompareAndSet(key, U64Bytes(idx), item.Counter()) == nil {
			return idx
		}
	}
	panic("Incr Group IDX Failed")
}

func (s *BFTRaftServer)SetGroupLogLastIndex(groupId uint64, idx uint64)  {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	s.DB.Set(key, U64Bytes(idx), 0x00)
}

func (s *BFTRaftServer) SaveGroup(group *pb.RaftGroup) {
	if data, err := proto.Marshal(group); err == nil {
		dbKey := append(ComposeKeyPrefix(group.Id, GROUP_META))
		s.DB.Set(dbKey, data, 0x00)
	}
}
