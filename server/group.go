package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
	"github.com/tevino/abool"
	"log"
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
	IsBusy          *abool.AtomicBool
}

func GetGroupFromKV(txn *badger.Txn, groupId uint64) *pb.RaftGroup {
	group := &pb.RaftGroup{}
	keyPrefix := ComposeKeyPrefix(groupId, GROUP_META)
	if item, err := txn.Get(keyPrefix); err == nil {
		data := ItemValue(item)
		if data == nil {
			return nil
		} else {
			proto.Unmarshal(*data, group)
			return group
		}
	} else {
		return nil
	}
}

func (s *BFTRaftServer) GetGroup(txn *badger.Txn, groupId uint64) *pb.RaftGroup {
	cacheKey := strconv.Itoa(int(groupId))
	cachedGroup, cacheFound := s.Groups.Get(cacheKey)
	if cacheFound {
		return cachedGroup.(*pb.RaftGroup)
	} else {
		group := GetGroupFromKV(txn, groupId)
		if group != nil {
			s.Groups.Set(cacheKey, group, cache.DefaultExpiration)
			return group
		} else {
			return nil
		}
	}
}

func (s *BFTRaftServer) GetGroupNTXN(groupId uint64) *pb.RaftGroup {
	group := &pb.RaftGroup{}
	s.DB.View(func(txn *badger.Txn) error {
		group = s.GetGroup(txn, groupId)
		return nil
	})
	return group
}

func (s *BFTRaftServer) GetGroupLogLastIndex(txn *badger.Txn, groupId uint64) uint64 {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	if item, err := txn.Get(key); err == nil {
		data := ItemValue(item)
		var idx uint64 = 0
		if data != nil {
			idx = utils.BytesU64(*data, 0)
		}
		return idx
	} else {
		log.Println(err)
		return 0
	}
}

func (s *BFTRaftServer) GetGroupLogLastIndexNTXN(groupId uint64) uint64 {
	var index uint64 = 0
	s.DB.View(func(txn *badger.Txn) error {
		index = s.GetGroupLogLastIndex(txn, groupId)
		return nil
	})
	return index
}

func (s *BFTRaftServer) IncrGetGroupLogLastIndex(txn *badger.Txn, groupId uint64) uint64 {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	if item, err := txn.Get(key); err == nil {
		data := ItemValue(item)
		idx := utils.BytesU64(*data, 0)
		idx += 1
		if txn.Set(key, utils.U64Bytes(idx), 0x00) == nil {
			return idx
		}
	} else if err == badger.ErrKeyNotFound {
		if txn.Set(key, utils.U64Bytes(1), 0x00) == nil {
			return 1
		}
	}
	return 0
}

func (s *BFTRaftServer) SetGroupLogLastIndex(txn *badger.Txn, groupId uint64, idx uint64) error {
	key := ComposeKeyPrefix(groupId, GROUP_LAST_IDX)
	return txn.Set(key, utils.U64Bytes(idx), 0x00)
}

func (s *BFTRaftServer) SaveGroup(txn *badger.Txn, group *pb.RaftGroup) error {
	if data, err := proto.Marshal(group); err == nil {
		dbKey := ComposeKeyPrefix(group.Id, GROUP_META)
		return txn.Set(dbKey, data, 0x00)
	} else {
		return err
	}
}

func (s *BFTRaftServer) SaveGroupNTXN(group *pb.RaftGroup) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		return s.SaveGroup(txn, group)
	})
}

func (s *BFTRaftServer) GetGroupHosts(groupId uint64) []*pb.Host {
	result := []*pb.Host{}
	s.DB.View(func(txn *badger.Txn) error {
		nodes := []*pb.Host{}
		peers := GetGroupPeersFromKV(txn, groupId)
		for _, peer := range peers {
			node := s.GetHost(txn, peer.Host)
			if node != nil {
				nodes = append(nodes, node)
			} else {
				log.Println(s.Id, "cannot find group node:", peer.Id)
			}
		}
		result = nodes
		return nil
	})
	return result
}
