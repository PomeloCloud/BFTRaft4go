package server

import (
	"bytes"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
)

type LogEntryIterator struct {
	prefix []byte
	data   *badger.Iterator
}

func (liter *LogEntryIterator) Next() *pb.LogEntry {
	liter.data.Next()
	if liter.data.ValidForPrefix(liter.prefix) {
		entry := pb.LogEntry{}
		itemData := ItemValue(liter.data.Item())
		if itemData == nil {
			return nil
		}
		proto.Unmarshal(*itemData, &entry)
		return &entry
	} else {
		return nil
	}
}

func (liter *LogEntryIterator) Close() {
	liter.data.Close()
}

func (s *BFTRaftServer) ReversedLogIterator(group uint64) LogEntryIterator {
	keyPrefix := ComposeKeyPrefix(group, LOG_ENTRIES)
	iter := s.DB.NewIterator(badger.IteratorOptions{Reverse: true})
	iter.Seek(append(keyPrefix, U64Bytes(^uint64(0))...)) // search from max possible index
	return LogEntryIterator{
		prefix: keyPrefix,
		data:   iter,
	}
}

func (s *BFTRaftServer) LastLogEntry(group uint64) *pb.LogEntry {
	iter := s.ReversedLogIterator(group)
	entry := iter.Next()
	iter.Close()
	return entry
}

func (s *BFTRaftServer) LastEntryHash(group_id uint64) []byte {
	var hash []byte
	lastLog := s.LastLogEntry(group_id)
	if lastLog == nil {
		hash, _ = SHA1Hash([]byte(fmt.Sprint("GROUP:", group_id)))
	}
	return hash
}

func (s *BFTRaftServer) AppendEntryToLocal(group *pb.RaftGroup, entry pb.LogEntry) error {
	group_id := entry.Command.Group
	key := append(ComposeKeyPrefix(group_id, LOG_ENTRIES), U64Bytes(entry.Index)...)
	existed, err := s.DB.Exists(key)
	if err != nil {
		return err
	} else if existed {
		return error("Entry existed")
	} else {
		hash, _ := SHA1Hash(s.LastEntryHash(group_id))
		if !bytes.Equal(hash, entry.Hash) {
			return error("Log entry hash mismatch")
		}
		if data, err := proto.Marshal(&entry); err != nil {
			s.DB.Set(key, data, 0x00)
			return nil
		} else {
			return err
		}
	}
}
