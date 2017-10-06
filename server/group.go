package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"strconv"
	"time"
)

type RTGroupMeta struct {
	LeaderLastSeen time.Time
}

func (s *BFTRaftServer) GetGroup(groupId uint64) *pb.RaftGroup {
	cacheKey := strconv.Itoa(int(groupId))
	cachedGroup, cacheFound := s.Groups.Get(cacheKey)
	if cacheFound {
		return cachedGroup.(*pb.RaftGroup)
	} else {
		group := &pb.RaftGroup{}
		keyPrefix := ComposeKeyPrefix(groupId, GROUP_META)
		item := badger.KVItem{}
		s.DB.Get(keyPrefix, &item)
		data := ItemValue(&item)
		if data == nil {
			return nil
		} else {
			proto.Unmarshal(*data, group)
			s.Groups.Set(cacheKey, group, cache.DefaultExpiration)
			return group
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
