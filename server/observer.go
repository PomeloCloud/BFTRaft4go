package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"context"
	"github.com/dgraph-io/badger"
	"log"
)

func (s *BFTRaftServer) PullAndCommitGroupLogs(groupId uint64) {
	meta, foundMeta := s.GroupsOnboard[groupId]
	if !foundMeta {
		panic("meta not found for pull")
	}
	peerClients := []pb.BFTRaftClient{}
	for _, peer := range meta.GroupPeers {
		node := s.GetHostNTXN(peer.Host)
		if rpc, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			peerClients = append(peerClients, rpc)
		}
	}
	req := &pb.PullGroupLogsResuest{
		Group: groupId,
		Index: s.GetGroupLogLastIndexNTXN(groupId) + 1,
	}
	// Pull entries
	entries := utils.MajorityResponse(peerClients, func(client pb.BFTRaftClient) (interface{}, []byte) {
		if entriesRes, err := client.PullGroupLogs(context.Background(), req); err != nil {
			entries := entriesRes.Entries
			if len(entries) == 0 {
				return entries, []byte{1}
			} else {
				return entries, entries[len(entries) - 1].Hash
			}
		}
		return nil, []byte{}
	}).([]*pb.LogEntry)
	// now append and commit logs one by one
	for _, entry := range entries {
		needCommit := false
		if err := s.DB.Update(func(txn *badger.Txn) error {
			if err := s.AppendEntryToLocal(txn, meta.Group, entry); err == nil {
				s.IncrGetGroupLogLastIndex(txn, groupId)
				needCommit = true
				return nil
			} else {
				return err
			}
		}); err != nil {
			log.Println(err)
			return
		}
		if needCommit {
			s.CommitGroupLog(groupId, entry)
		}
	}
}
