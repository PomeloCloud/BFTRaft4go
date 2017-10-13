package server

import (
	"bytes"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"log"
)

type LogEntryIterator struct {
	prefix []byte
	data   *badger.Iterator
}

func LogEntryFromKVItem(item *badger.Item) *pb.LogEntry {
	entry := pb.LogEntry{}
	itemData := ItemValue(item)
	if itemData == nil {
		return nil
	}
	proto.Unmarshal(*itemData, &entry)
	return &entry
}

func (liter *LogEntryIterator) Next() *pb.LogEntry {
	if liter.data.Valid() {
		liter.data.Next()
		if liter.data.ValidForPrefix(liter.prefix) {
			return LogEntryFromKVItem(liter.data.Item())
		} else {
			return nil
		}
	} else {
		return nil
	}
}

func (liter *LogEntryIterator) Close() {
	liter.data.Close()
}

func (s *BFTRaftServer) ReversedLogIterator(txn *badger.Txn, group uint64) LogEntryIterator {
	keyPrefix := ComposeKeyPrefix(group, LOG_ENTRIES)
	iter := txn.NewIterator(badger.IteratorOptions{Reverse: true})
	iter.Seek(append(keyPrefix, utils.U64Bytes(^uint64(0))...)) // search from max possible index
	return LogEntryIterator{
		prefix: keyPrefix,
		data:   iter,
	}
}

func (s *BFTRaftServer) LastLogEntry(txn *badger.Txn, group uint64) *pb.LogEntry {
	iter := s.ReversedLogIterator(txn, group)
	entry := iter.Next()
	iter.Close()
	return entry
}

func (s *BFTRaftServer) LastLogEntryNTXN(group uint64) *pb.LogEntry {
	entry := &pb.LogEntry{}
	s.DB.View(func(txn *badger.Txn) error {
		iter := s.ReversedLogIterator(txn, group)
		entry = iter.Next()
		iter.Close()
		return nil
	})
	return entry
}

func (s *BFTRaftServer) LastEntryHash(txn *badger.Txn, group_id uint64) []byte {
	var hash []byte
	lastLog := s.LastLogEntry(txn, group_id)
	if lastLog == nil {
		hash, _ = utils.SHA1Hash([]byte(fmt.Sprint("GROUP:", group_id)))
	} else {
		hash = lastLog.Hash
	}
	return hash
}

func (s *BFTRaftServer) LastEntryHashNTXN(group_id uint64) []byte {
	hash := []byte{}
	s.DB.View(func(txn *badger.Txn) error {
		hash = s.LastEntryHash(txn, group_id)
		return nil
	})
	return hash
}

func LogEntryKey(groupId uint64, entryIndex uint64) []byte {
	return append(ComposeKeyPrefix(groupId, LOG_ENTRIES), utils.U64Bytes(entryIndex)...)
}

func (s *BFTRaftServer) AppendEntryToLocal(txn *badger.Txn, group *pb.RaftGroup, entry *pb.LogEntry) error {
	group_id := entry.Command.Group
	key := LogEntryKey(group_id, entry.Index)
	_, err := txn.Get(key)
	if err == badger.ErrKeyNotFound {
		cmd := entry.Command
		hash, _ := utils.LogHash(s.LastEntryHash(txn, group_id), entry.Index, cmd.FuncId, cmd.Arg)
		if !bytes.Equal(hash, entry.Hash) {
			return errors.New("Log entry hash mismatch")
		}
		if data, err := proto.Marshal(entry); err == nil {
			txn.Set(key, data, 0x00)
			return nil
		} else {
			log.Println("cannot append log to local:", err)
			return err
		}
	} else if err == nil {
		return errors.New("log existed")
	} else {
		return err
	}
}

func (s *BFTRaftServer) GetLogEntry(txn *badger.Txn, groupId uint64, entryIndex uint64) *pb.LogEntry {
	key := LogEntryKey(groupId, entryIndex)
	if item, err := txn.Get(key); err == nil {
		return LogEntryFromKVItem(item)
	} else {
		return nil
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
	bs1 := append(utils.U64Bytes(res.Peer), EB(res.Appended), EB(res.Delayed), EB(res.Failed))
	return append(bs1, utils.U64Bytes(res.Index)...)
}

func (s *BFTRaftServer) CommitGroupLog(groupId uint64, entry *pb.LogEntry) *[]byte {
	funcId := entry.Command.FuncId
	fun := s.FuncReg[groupId][funcId]
	input := entry.Command.Arg
	result := fun(&input, entry)
	return &result
}
