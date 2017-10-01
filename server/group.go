package server

import (
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"strconv"
	"github.com/patrickmn/go-cache"
)

func (s *BFTRaftServer) GetGroup(group_id uint64) *pb.RaftGroup {
	cacheKey := strconv.Itoa(int(group_id))
	cachedGroup, cacheFound := s.Groups.Get(cacheKey)
	if cacheFound {
		return cachedGroup.(*pb.RaftGroup)
	} else {
		group := &pb.RaftGroup{}
		keyPrefix := ComposeKeyPrefix(group_id, GROUP_META)
		item := badger.KVItem{}
		s.DB.Get(keyPrefix, &item)
		proto.Unmarshal(ItemValue(&item), group)
		s.Groups.Set(cacheKey, group, cache.DefaultExpiration)
		return group
	}
}