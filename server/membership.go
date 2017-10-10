package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/golang/protobuf/proto"
	"log"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
)

const (
	NODE_JOIN  = 0
	REG_NODE   = 1
	NEW_CLIENT = 2
	NODE_GROUP = 3
)

func (s *BFTRaftServer) RegisterMembershipCommands() {
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NODE_JOIN, s.NodeJoin)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, REG_NODE, s.RegNode)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NEW_CLIENT, s.NewClient)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NODE_GROUP, s.NewGroup)
}


// Register a node into the network
// The node may be new or it was rejoined with new address
func (s *BFTRaftServer) RegNode(arg *[]byte, entry *pb.LogEntry) []byte {
	node := pb.Node{}
	if err := proto.Unmarshal(*arg, &node); err == nil {
		node.Id = HashPublicKeyBytes(node.PublicKey)
		node.Online = true
		s.DB.Update(func(txn *badger.Txn) error {
			s.SaveNode(txn, &node)
			return nil
		})
		return []byte{1}
	}  else {
		log.Println(err)
		return []byte{0}
	}
}

func (s *BFTRaftServer) NodeJoin(arg *[]byte, entry *pb.LogEntry) []byte {
	req := pb.NodeJoinGroupEntry{}
	if err := proto.Unmarshal(*arg, &req); err == nil {
		node := entry.Command.ClientId
		// this should be fine, a public key can use both for client and node
		group := req.Group
		if node == s.Id {
			// skip if current node is the joined node
			// when joined a group, the node should do all of
			// those following things by itself after the log is replicated
			return []byte{2}
		}
		peer := pb.Peer{
			Id: node,
			Group: group,
			Host: node,
			NextIndex: 0,
			MatchIndex: 0,
		}
		// first, save the peer
		s.DB.Update(func(txn *badger.Txn) error {
			return s.SavePeer(txn, &peer)
		})
		// next, check if this node is in the group. Add it on board if found.
		// because membership logs entries will be replicated on every node
		// this function will also be executed every where
		if meta, found := s.GroupsOnboard[group]; found {
			meta.Lock.Lock()
			defer meta.Lock.Unlock()
			meta.GroupPeers[peer.Id] = &peer
		}
		return []byte{1}
	} else {
		return []byte{0}
	}
}


func (s *BFTRaftServer) NewClient(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}

func (s *BFTRaftServer) NewGroup(arg *[]byte, entry *pb.LogEntry) []byte {
	return []byte{}
}
