package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"context"
	"github.com/dgraph-io/badger"
	"log"
)

func (m *RTGroup) PullAndCommitGroupLogs() {
	peerClients := []*pb.BFTRaftClient{}
	for _, peer := range m.GroupPeers {
		node := m.Server.GetHostNTXN(peer.Id)
		if rpc, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			peerClients = append(peerClients, &rpc)
		}
	}
	req := &pb.PullGroupLogsResuest{
		Group: m.Group.Id,
		Index: m.LastEntryIndexNTXN() + 1,
	}
	// Pull entries
	entries := utils.MajorityResponse(peerClients, func(client pb.BFTRaftClient) (interface{}, []byte) {
		if entriesRes, err := client.PullGroupLogs(context.Background(), req); err == nil {
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
		if err := m.Server.DB.Update(func(txn *badger.Txn) error {
			if err := m.AppendEntryToLocal(txn, entry); err == nil {
				needCommit = true
				return nil
			} else {
				return err
			}
		}); err != nil {
			log.Println("cannot append entry to local when pulling", err)
			return
		}
		if needCommit {
			m.CommitGroupLog(entry)
		}
	}
}
