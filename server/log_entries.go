package server

import (
	"bytes"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/docker/docker/pkg/testutil/cmd"
)

type LogEntryIterator struct {
	prefix []byte
	data   *badger.Iterator
}

type LogAppendError struct {
	msg string
}

func (e *LogAppendError) Error() string {
	return fmt.Sprintf("%s", e.msg)
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
	} else {
		hash = lastLog.Hash
	}
	return hash
}

func LogEntryKey(groupId uint64, entryIndex uint64) []byte {
	return append(ComposeKeyPrefix(groupId, LOG_ENTRIES), U64Bytes(entryIndex)...)
}

func (s *BFTRaftServer) AppendEntryToLocal(group *pb.RaftGroup, entry *pb.LogEntry) error {
	group_id := entry.Command.Group
	key := LogEntryKey(group_id, entry.Index)
	existed, err := s.DB.Exists(key)
	if err != nil {
		return err
	} else if !existed {
		cmd := entry.Command
		hash, _ := LogHash(s.LastEntryHash(group_id), entry.Index, cmd.FuncId, cmd.Arg)
		if !bytes.Equal(hash, entry.Hash) {
			return &LogAppendError{"Log entry hash mismatch"}
		}
		if data, err := proto.Marshal(entry); err != nil {
			s.DB.Set(key, data, 0x00)
			return nil
		} else {
			return err
		}
	} else {
		return nil
	}
}

func (s *BFTRaftServer) GetLogEntry(groupId uint64, entryIndex uint64) *pb.LogEntry {
	key := LogEntryKey(groupId, entryIndex)
	item := badger.KVItem{}
	s.DB.Get(key, &item)
	data := ItemValue(&item)
	if data == nil {
		return nil
	} else {
		entry := pb.LogEntry{}
		proto.Unmarshal(*data, &entry)
		return &entry
	}
}

func AppendLogEntrySignData(groupId uint64, term uint64, prevIndex uint64, prevTerm uint64) []byte {
	return []byte(fmt.Sprint(groupId, "-", term, "-", prevIndex, "-", prevTerm))
}

func EB(b bool) byte {
	if b {
		return byte(1)
	} else {
		return byte(0)
	}
}

func ApproveAppendSignData(res *pb.ApproveAppendResponse) []byte {
	bs1 := append(U64Bytes(res.Peer), EB(res.Appended), EB(res.Delayed), EB(res.Failed))
	return append(bs1, U64Bytes(res.Index)...)
}

func (s *BFTRaftServer) CommitGroupLog(groupId uint64, entry *pb.LogEntry) *[]byte {
	funcId := entry.Command.FuncId
	fun := s.FuncReg[groupId][funcId]
	input := entry.Command.Arg
	result := fun(&input, entry)
	return &result
}
