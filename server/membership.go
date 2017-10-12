package server

import (
	"crypto/x509"
	"errors"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"log"
)

const (
	NODE_JOIN  = 0
	REG_NODE   = 1
	NEW_CLIENT = 2
	NODE_GROUP = 3
)

func (s *BFTRaftServer) RegisterMembershipCommands() {
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NODE_JOIN, s.SMNodeJoin)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, REG_NODE, s.SMRegHost)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NEW_CLIENT, s.SMNewClient)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NODE_GROUP, s.SMNewGroup)
}

// Register a node into the network
// The node may be new or it was rejoined with new address
func (s *BFTRaftServer) SMRegHost(arg *[]byte, entry *pb.LogEntry) []byte {
	node := pb.Host{}
	if err := proto.Unmarshal(*arg, &node); err == nil {
		node.Id = utils.HashPublicKeyBytes(node.PublicKey)
		node.Online = true
		nodeClient := pb.Host{
			Id:         node.Id,
			ServerAddr: node.ServerAddr,
			PublicKey:  node.PublicKey,
		}
		s.DB.Update(func(txn *badger.Txn) error {
			if err := s.SaveHost(txn, &node); err == nil {
				return s.SaveHost(txn, &nodeClient)
			} else {
				return err
			}
		})
		return []byte{1}
	} else {
		log.Println(err)
		return []byte{0}
	}
}

func (s *BFTRaftServer) SMNodeJoin(arg *[]byte, entry *pb.LogEntry) []byte {
	req := pb.NodeJoinGroupEntry{}
	if err := proto.Unmarshal(*arg, &req); err == nil {
		node := entry.Command.ClientId
		// this should be fine, a public key can use both for client and node
		groupId := req.Group
		peer := pb.Peer{
			Id:         node,
			Group:      groupId,
			Host:       node,
			NextIndex:  0,
			MatchIndex: 0,
		}
		if err := s.DB.Update(func(txn *badger.Txn) error {
			if s.GetHost(txn, node) == nil {
				return errors.New("cannot find node")
			}
			if node == s.Id {
				// skip if current node is the joined node
				// when joined a groupId, the node should do all of
				// those following things by itself after the log is replicated
				return errors.New("join should be processed")
			}
			group := s.GetGroup(txn, groupId)
			// check if this group exceeds it's replication
			if len(GetGroupPeersFromKV(txn, groupId)) >= int(group.Replications) {
				return errors.New("exceed replications")
			}
			// first, save the peer
			return s.SavePeer(txn, &peer)
		}); err != nil {
			log.Println(err)
			return []byte{0}
		}
		// next, check if this node is in the groupId. Add it on board if found.
		// because membership logs entries will be replicated on every node
		// this function will also be executed every where
		if meta, found := s.GroupsOnboard[groupId]; found {
			meta.Lock.Lock()
			defer meta.Lock.Unlock()
			meta.GroupPeers[node] = &peer
		}
		return []byte{1}
	} else {
		return []byte{0}
	}
}

func (s *BFTRaftServer) SMNewClient(arg *[]byte, entry *pb.LogEntry) []byte {
	// use for those hosts only want to make changes, and does not contribute it's resources
	client := pb.Host{}
	err := proto.Unmarshal(*arg, &client)
	if err != nil {
		log.Println(err)
		return []byte{0}
	}
	client.Id = utils.HashPublicKeyBytes(client.PublicKey)
	if err := s.SaveHostNTXN(&client); err != nil {
		return []byte{1}
	} else {
		log.Println(err)
		return []byte{0}
	}
}

func (s *BFTRaftServer) SMNewGroup(arg *[]byte, entry *pb.LogEntry) []byte {
	hostId := entry.Command.ClientId
	// create and make the creator the member of this group
	group := pb.RaftGroup{}
	err := proto.Unmarshal(*arg, &group)
	if err != nil {
		log.Println(err)
		return []byte{0}
	}
	// replication cannot be below 1 and cannot larger than 100
	if group.Replications < 1 || group.Replications > 100 {
		return []byte{0}
	}
	// generate peer
	peer := pb.Peer{
		Id:         hostId,
		Group:      group.Id,
		Host:       hostId,
		NextIndex:  0,
		MatchIndex: 0,
	}
	if err := s.DB.Update(func(txn *badger.Txn) error {
		// the proposer will decide the id for the group, we need to check it's availability
		if s.GetGroup(txn, group.Id) != nil {
			return errors.New("group existed")
		}
		// regularize and save group
		group.Term = 0
		group.LeaderPeer = hostId
		if err := s.SaveGroup(txn, &group); err != nil {
			return err
		}
		if err := s.SavePeer(txn, &peer); err == nil {
			return err
		}
		return nil
	}); err != nil {
		return []byte{1}
	} else {
		log.Println(err)
		return []byte{0}
	}
}

func (s *BFTRaftServer) RegHost() error {
	groupId := uint64(utils.ALPHA_GROUP)
	publicKey := utils.PublicKeyFromPrivate(s.PrivateKey)
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}
	host := pb.Host{
		Id:         s.Id,
		LastSeen:   0,
		Online:     true,
		ServerAddr: s.Opts.Address,
		PublicKey:  publicKeyBytes,
	}
	hostData, err := proto.Marshal(&host)
	if err != nil {
		return err
	}
	res, err := s.Client.ExecCommand(groupId, REG_NODE, hostData)
	if err != nil {
		return err
	}
	switch (*res)[0] {
	case 0:
		return errors.New("remote error")
	case 1:
		return nil
	}
	return errors.New("unexpected")
}

func (s *BFTRaftServer) NodeJoin(groupId uint64) error {
	joinEntry := pb.NodeJoinGroupEntry{Group: groupId}
	joinData, err := proto.Marshal(&joinEntry)
	if err != nil {
		return err
	}
	res, err := s.Client.ExecCommand(utils.ALPHA_GROUP, NODE_JOIN, joinData)
	if err != nil {
		return err
	}
	switch (*res)[0] {
	case 0:
		return errors.New("remote error")
	case 1:
		// add self into the group

	}
}
