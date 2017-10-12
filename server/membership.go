package server

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"github.com/tevino/abool"
	"log"
	"sync"
	"time"
)

const (
	NODE_JOIN  = 0
	REG_NODE   = 1
	NEW_CLIENT = 2
	NEW_GROUP  = 3
)

func (s *BFTRaftServer) RegisterMembershipCommands() {
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NODE_JOIN, s.SMNodeJoin)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, REG_NODE, s.SMRegHost)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NEW_CLIENT, s.SMNewClient)
	s.RegisterRaftFunc(utils.ALPHA_GROUP, NEW_GROUP, s.SMNewGroup)
}

// Register a node into the network
// The node may be new or it was rejoined with new address
func (s *BFTRaftServer) SMRegHost(arg *[]byte, entry *pb.LogEntry) []byte {
	node := pb.Host{}
	if err := proto.Unmarshal(*arg, &node); err == nil {
		node.Id = utils.HashPublicKeyBytes(node.PublicKey)
		node.Online = true
		s.DB.Update(func(txn *badger.Txn) error {
			if err := s.SaveHost(txn, &node); err == nil {
				return nil
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
			address := s.GetHostNTXN(entry.Command.ClientId).ServerAddr
			inv := &pb.GroupInvitation{
				Group:  groupId,
				Leader: meta.Group.LeaderPeer,
				Node:   meta.Peer,
			}
			inv.Signature = s.Sign(InvitationSignature(inv))
			if client, err := utils.GetClusterRPC(address); err != nil {
				go client.SendGroupInvitation(context.Background(), inv)
				s.GroupsOnboard[groupId].GroupPeers[peer.Id] = &peer
				return []byte{1}
			} else {
				log.Println(err)
				return []byte{0}
			}
			meta.GroupPeers[node] = &peer
		}
		return []byte{1}
	} else {
		log.Println(err)
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

func InvitationSignature(inv *pb.GroupInvitation) []byte {
	return []byte(fmt.Sprint(inv.Group, "-", inv.Node, "-", inv.Leader))
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
		if s.Id == hostId {
			s.GroupsOnboard[peer.Group] = &RTGroupMeta{
				Peer:       peer.Id,
				Leader:     group.LeaderPeer,
				Lock:       sync.RWMutex{},
				GroupPeers: map[uint64]*pb.Peer{peer.Id: &peer},
				Group:      &group,
				IsBusy:     abool.NewBool(false),
			}
			s.PendingNewGroups[group.Id] <- nil
		}
		return utils.U64Bytes(entry.Index)
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
	if (*res)[0] == 1 {
		hosts := s.GetGroupHosts(groupId)
		hostsMap := map[uint64]bool{}
		for _, h := range hosts {
			hostsMap[h.Id] = true
		}
		receivedEnoughInv := make(chan bool, 1)
		invitations := map[uint64]bool{}
		expectedResponse := len(hostsMap) / 2
		go func() {
			for invHost := range s.GroupInvitations[groupId] {
				if _, isMember := hostsMap[invHost]; isMember {
					invitations[invHost] = true
					if len(invitations) > expectedResponse {
						receivedEnoughInv <- true
					}
				}
			}
		}()
		select {
		case <-receivedEnoughInv:
			group := s.GetGroupNTXN(groupId)
			peer := &pb.Peer{
				Id:         s.Id,
				Group:      groupId,
				Host:       s.Id,
				NextIndex:  0,
				MatchIndex: 0,
			}
			groupPeers := map[uint64]*pb.Peer{}
			if err := s.DB.Update(func(txn *badger.Txn) error {
				groupPeers = GetGroupPeersFromKV(txn, peer.Group)
				return s.SavePeer(txn, peer)
			}); err == nil {
				groupPeers[s.Id] = peer
				s.GroupsOnboard[peer.Group] = &RTGroupMeta{
					Peer:       peer.Id,
					Leader:     group.LeaderPeer,
					Lock:       sync.RWMutex{},
					GroupPeers: groupPeers,
					Group:      group,
					IsBusy:     abool.NewBool(false),
				}
			}
			return nil
		case <-time.After(30 * time.Second):
			close(s.GroupInvitations[groupId])
			delete(s.GroupInvitations, groupId)
			return errors.New("receive invitation timeout")
		}
	} else {
		return errors.New("remote error")
	}
}

func (s *BFTRaftServer) NewGroup(group *pb.RaftGroup) error {
	groupData, err := proto.Marshal(group)
	if err == nil {
		return err
	}
	s.PendingNewGroups[group.Id] = make(chan error, 1)
	res, err := s.Client.ExecCommand(utils.ALPHA_GROUP, NEW_GROUP, groupData)
	if err == nil {
		return err
	}
	if len(*res) > 1 {
		// wait for the log to be committed
		select {
		case err := <-s.PendingNewGroups[group.Id]:
			return err
		case <-time.After(10 * time.Second):
			return errors.New("timeout")
		}
	} else {
		return errors.New("remote error")
	}
}
