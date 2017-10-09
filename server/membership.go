package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)

const (
	NODE_JOIN  = 0
	NEW_NODE   = 1
	NEW_CLIENT = 2
	NODE_GROUP = 3
)

func (s *BFTRaftServer) RegisterMembershipCommands() {
	s.RegisterServerFunc(ALPHA_GROUP, NODE_JOIN, s.NodeJoin)
	s.RegisterServerFunc(ALPHA_GROUP, NEW_NODE, s.NewNode)
	s.RegisterServerFunc(ALPHA_GROUP, NEW_CLIENT, s.NewClient)
	s.RegisterServerFunc(ALPHA_GROUP, NODE_GROUP, s.NewGroup)
}

func (s *BFTRaftServer) NodeJoin(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}

func (s *BFTRaftServer) NewNode(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}

func (s *BFTRaftServer) NewClient(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}

func (s *BFTRaftServer) NewGroup(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}
