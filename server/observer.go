package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"context"
)

func (s *BFTRaftServer) PullAndCommitGroupLogs(groupId uint64) {
	meta, foundMeta := s.GroupsOnboard[groupId]
	if !foundMeta {
		panic("meta not found for pull")
	}
	peerClients := []pb.BFTRaftClient{}
	for _, peer := range meta.GroupPeers {
		node := s.GetNode(peer.Host)
		if rpc, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			peerClients = append(peerClients, rpc)
		}
	}
	req := &pb.PullGroupLogsResuest{
		Group: groupId,
		Index: s.GetGroupLogLastIndex(groupId) + 1,
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
	// now append and commit logs on by one
	for _, entry := range entries {
		if err := s.AppendEntryToLocal(meta.Group, entry); err == nil {
			s.IncrGetGroupLogLastIndex(groupId)
			s.CommitGroupLog(groupId, entry.Command)
		} else {
			panic(err)
		}
	}
}
