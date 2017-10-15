package server

import (
	"bytes"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
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

func (liter *LogEntryIterator) Current() *pb.LogEntry {
	if liter.data.ValidForPrefix(liter.prefix) {
		return LogEntryFromKVItem(liter.data.Item())
	} else {
		return nil
	}
}

func (liter *LogEntryIterator) Next() *pb.LogEntry {
	if liter.data.Valid() {
		liter.data.Next()
		return liter.Current()
	} else {
		return nil
	}
}

func (liter *LogEntryIterator) Close() {
	liter.data.Close()
}

func (m *RTGroup) ReversedLogIterator(txn *badger.Txn) LogEntryIterator {
	keyPrefix := ComposeKeyPrefix(m.Group.Id, LOG_ENTRIES)
	iter := txn.NewIterator(badger.IteratorOptions{Reverse: true})
	iter.Seek(append(keyPrefix, utils.U64Bytes(^uint64(0))...)) // search from max possible index
	return LogEntryIterator{
		prefix: keyPrefix,
		data:   iter,
	}
}

func (m *RTGroup) LastLogEntry(txn *badger.Txn) *pb.LogEntry {
	iter := m.ReversedLogIterator(txn)
	entry := iter.Current()
	iter.Close()
	return entry
}

func (m *RTGroup) LastLogEntryNTXN() *pb.LogEntry {
	entry := &pb.LogEntry{}
	m.Server.DB.View(func(txn *badger.Txn) error {
		iter := m.ReversedLogIterator(txn)
		entry = iter.Current()
		iter.Close()
		return nil
	})
	return entry
}

func (m *RTGroup) LastEntryHash(txn *badger.Txn) []byte {
	var hash []byte
	lastLog := m.LastLogEntry(txn)
	if lastLog == nil {
		hash, _ = utils.SHA1Hash([]byte(fmt.Sprint("GROUP:", m.Group.Id)))
	} else {
		hash = lastLog.Hash
	}
	return hash
}

func (m *RTGroup) LastEntryIndex(txn *badger.Txn) uint64 {
	lastLog := m.LastLogEntry(txn)
	index := uint64(0)
	if lastLog != nil {
		index = lastLog.Index
	}
	return index
}

func (m *RTGroup) LastEntryIndexNTXN() uint64 {
	index := uint64(0)
	m.Server.DB.View(func(txn *badger.Txn) error {
		index = m.LastEntryIndex(txn)
		return nil
	})
	return index
}

func (m *RTGroup) LastEntryHashNTXN() []byte {
	hash := []byte{}
	m.Server.DB.View(func(txn *badger.Txn) error {
		hash = m.LastEntryHash(txn)
		return nil
	})
	return hash
}

func LogEntryKey(groupId uint64, entryIndex uint64) []byte {
	return append(ComposeKeyPrefix(groupId, LOG_ENTRIES), utils.U64Bytes(entryIndex)...)
}

func (m *RTGroup) AppendEntryToLocal(txn *badger.Txn, entry *pb.LogEntry) error {
	group_id := entry.Command.Group
	if group_id != m.Group.Id {
		panic("log group id not match the actual group work on it")
	}
	key := LogEntryKey(group_id, entry.Index)
	_, err := txn.Get(key)
	if err == badger.ErrKeyNotFound {
		cmd := entry.Command
		hash, _ := utils.LogHash(m.LastEntryHash(txn), entry.Index, cmd.FuncId, cmd.Arg)
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

func (m *RTGroup) GetLogEntry(txn *badger.Txn, entryIndex uint64) *pb.LogEntry {
	key := LogEntryKey(m.Group.Id, entryIndex)
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

func (m *RTGroup) CommitGroupLog(groupId uint64, entry *pb.LogEntry) *[]byte {
	funcId := entry.Command.FuncId
	fun := m.Server.FuncReg[groupId][funcId]
	input := entry.Command.Arg
	result := fun(&input, entry)
	return &result
}
