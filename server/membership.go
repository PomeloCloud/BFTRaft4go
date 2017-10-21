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
	"log"
	"time"
)

const (
	NODE_JOIN  = 0
	REG_NODE   = 1
	NEW_CLIENT = 2
	NEW_GROUP  = 3
)

func (s *BFTRaftServer) RegisterMembershipCommands() {
	s.RegisterRaftFunc(NODE_JOIN, s.SMNodeJoin)
	s.RegisterRaftFunc(REG_NODE, s.SMRegHost)
	s.RegisterRaftFunc(NEW_CLIENT, s.SMNewClient)
	s.RegisterRaftFunc(NEW_GROUP, s.SMNewGroup)
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
				log.Println("error on saving host:", err)
				return err
			}
		})
		log.Println("we have node", node.Id, "on the network")
		return []byte{1}
	} else {
		log.Println("error on decoding reg host:", err)
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
			NextIndex:  0,
			MatchIndex: 0,
		}
		log.Println("SM node", node, "join", groupId)
		if err := s.DB.Update(func(txn *badger.Txn) error {
			if s.GetHost(txn, node) == nil {
				return errors.New("cannot find node")
			}
			if node == s.Id {
				// skip if current node is the joined node
				// when joined a groupId, the node should do all of
				// those following things by itself after the log is replicated
				log.Println("skip add current node join from sm")
				return nil
			}
			group := s.GetGroup(txn, groupId)
			// check if this group exceeds it's replication
			if len(GetGroupPeersFromKV(txn, groupId)) >= int(group.Replications) {
				return errors.New("exceed replications")
			}
			// first, save the peer
			return s.SavePeer(txn, &peer)
		}); err != nil {
			log.Println("cannot save peer for join", err)
			return []byte{0}
		}
		// next, check if this node is in the groupId. Add it on board if found.
		// because membership logs entries will be replicated on every node
		// this function will also be executed every where
		if meta := s.GetOnboardGroup(groupId); meta != nil {
			node := s.GetHostNTXN(entry.Command.ClientId)
			if node == nil {
				log.Println("cannot get node for SM node join")
			}
			address := node.ServerAddr
			inv := &pb.GroupInvitation{
				Group:  groupId,
				Leader: meta.Leader,
				Node:   meta.Server.Id,
			}
			inv.Signature = s.Sign(InvitationSignature(inv))
			if client, err := utils.GetClusterRPC(address); err == nil {
				go client.SendGroupInvitation(context.Background(), inv)
				meta.GroupPeers[peer.Id] = &peer
				log.Println("we have new node ", node.Id, "join group", groupId)
				return []byte{1}
			} else {
				log.Println("cannot get cluster rpc for node join", err)
				return []byte{0}
			}
		}
		return []byte{1}
	} else {
		log.Println("cannot decode node join data", err)
		return []byte{0}
	}
}

func (s *BFTRaftServer) SMNewClient(arg *[]byte, entry *pb.LogEntry) []byte {
	// use for those hosts only want to make changes, and does not contribute it's resources
	client := pb.Host{}
	err := proto.Unmarshal(*arg, &client)
	if err != nil {
		log.Println("cannot decode new client data", err)
		return []byte{0}
	}
	client.Id = utils.HashPublicKeyBytes(client.PublicKey)
	if err := s.SaveHostNTXN(&client); err == nil {
		return []byte{1}
	} else {
		log.Println("cannot save host for new client", err)
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
		log.Println("cannot decode new group data", err)
		return []byte{0}
	}
	log.Println("creating new group", group.Id,"for:", hostId)
	// replication cannot be below 1 and cannot larger than 100
	if group.Replications < 1 || group.Replications > 100 {
		log.Println("invalid replications:", group.Replications)
		return []byte{0}
	}
	// generate peer
	peer := pb.Peer{
		Id:         hostId,
		Group:      group.Id,
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
		if err := s.SaveGroup(txn, &group); err != nil {
			return err
		}
		if err := s.SavePeer(txn, &peer); err == nil {
			return err
		}
		return nil
	}); err == nil {
		if s.Id == hostId {
			meta := NewRTGroup(
				s, hostId,
				map[uint64]*pb.Peer{peer.Id: &peer},
				&group, LEADER,
			)
			s.SetOnboardGroup(meta)
			go func() {
				s.PendingNewGroups[group.Id] <- nil
			}()
		}
		return utils.U64Bytes(entry.Index)
	} else {
		go func() {
			s.PendingNewGroups[group.Id] <- err
		}()
		log.Println("cannot save new group:", err)
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
	s.SaveHostNTXN(&host) // save itself first
	res, err := s.Client.ExecCommand(groupId, REG_NODE, hostData)
	if err != nil {
		return err
	}
	switch (*res)[0] {
	case 0:
		return errors.New("remote error")
	case 1:
		log.Println("node", s.Id, "registed")
		return nil
	}
	return errors.New("unexpected")
}

func (s *BFTRaftServer) NodeJoin(groupId uint64) error {
	log.Println(s.Id, ": join group:", groupId)
	joinEntry := pb.NodeJoinGroupEntry{Group: groupId}
	joinData, err := proto.Marshal(&joinEntry)
	if err != nil {
		return err
	}
	s.GroupInvitations[groupId] = make(chan *pb.GroupInvitation)
	res, err := s.Client.ExecCommand(utils.ALPHA_GROUP, NODE_JOIN, joinData)
	if err != nil {
		log.Println("error on join group:", err)
		return err
	}
	if (*res)[0] == 1 {
		hosts := s.GetGroupHostsNTXN(groupId)
		hostsMap := map[uint64]bool{}
		for _, h := range hosts {
			hostsMap[h.Id] = true
		}
		receivedEnoughInv := make(chan bool, 1)
		invitations := map[uint64]*pb.GroupInvitation{}
		invLeaders := []uint64{}
		expectedResponse := len(hostsMap) / 2
		go func() {
			for inv := range s.GroupInvitations[groupId] {
				if _, isMember := hostsMap[inv.Node]; isMember {
					if _, hasInv := invitations[inv.Node]; !hasInv {
						invitations[inv.Node] = inv
						invLeaders = append(invLeaders, inv.Leader)
					}
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
				NextIndex:  0,
				MatchIndex: 0,
			}
			groupPeers := map[uint64]*pb.Peer{}
			if err := s.DB.Update(func(txn *badger.Txn) error {
				groupPeers = GetGroupPeersFromKV(txn, peer.Group)
				return s.SavePeer(txn, peer)
			}); err == nil {
				leader := utils.PickMajority(invLeaders)
				log.Println("received enough invitations, will join to group", groupId, "with leader", leader)
				groupPeers[s.Id] = peer
				role := FOLLOWER
				if leader == s.Id {
					log.Println("node", s.Id, "joined as a leader")
					leader = LEADER
				}
				meta := NewRTGroup(
					s, leader, groupPeers,
					group, role,
				)
				s.SetOnboardGroup(meta)
				log.Println("node", peer.Id, "joined group", groupId)
			}
			return nil
		case <-time.After(30 * time.Second):
			close(s.GroupInvitations[groupId])
			delete(s.GroupInvitations, groupId)
			return errors.New("receive invitation timeout")
		}
	} else {
		log.Println("cannot join node, remote end rejected")
		return errors.New("remote error")
	}
}

func (s *BFTRaftServer) NewGroup(group *pb.RaftGroup) error {
	groupData, err := proto.Marshal(group)
	if err != nil {
		return err
	}
	s.PendingNewGroups[group.Id] = make(chan error, 1)
	res, err := s.Client.ExecCommand(utils.ALPHA_GROUP, NEW_GROUP, groupData)
	if err != nil {
		log.Println("cannot decode new group:", err)
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
