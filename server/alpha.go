package server

import (
	"context"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"github.com/golang/protobuf/proto"
)

// Alpha group is a group specialized for tracking network members and groups
// All nodes on the network should observe alpha group to provide group routing
// Alpha group will not track leadership changes, only members
// It also responsible for group creation and limit number of members in each group
// Both clients and cluster nodes can benefit form alpha group by bootstrapping with any node in the cluster
// It also provide valuable information for consistent hashing and distributed hash table implementations

// This file contains all of the functions for cluster nodes to track changes in alpha group

func (s *BFTRaftServer) SyncAlphaGroup(bootstrap []string) {
	// Force a snapshot sync for group members by asking alpha nodes for it
	// This function should be invoked every time it startup
	// First we need to get all alpha nodes
	alphaNodes := utils.AlphaNodes(bootstrap)
	alphaRPCs := []spb.BFTRaftClient{}
	for _, node := range alphaNodes {
		if rpc, err := utils.GetClusterRPC(node.ServerAddr); err == nil {
			alphaRPCs = append(alphaRPCs, rpc)
		}
	}
	// get alpha peers from alpha nodes
	alphaPeersRes := utils.MajorityResponse(alphaRPCs, func(client spb.BFTRaftClient) (interface{}, []byte) {
		if res, err := client.GroupPeers(context.Background(), &spb.GroupId{
			GroupId: ALPHA_GROUP,
		}); err == nil {
			return res, GetPeersSignData(res.Peers)
		} else {
			return nil, []byte{}
		}
	}).(*spb.GroupPeersResponse)
	peers := alphaPeersRes.Peers
	isAlphaMember := false
	for _, p := range peers {
		if p.Host == s.Id {
			isAlphaMember = true
			break
		}
	}
	lastEntry := alphaPeersRes.LastEntry
	group := s.GetGroup(ALPHA_GROUP)
	if isAlphaMember {
		if group == nil {
			panic("Alpha member cannot find alpha group")
		}
		// Nothing should be done here, the raft algorithm should take the rest
	} else {
		if group == nil {
			// alpha group cannot be found, it need to be generated
			group = utils.MajorityResponse(alphaRPCs, func(client spb.BFTRaftClient) (interface{}, []byte) {
				if res, err := client.GetGroupContent(context.Background(), &spb.GroupId{ALPHA_GROUP}); err == nil {
					if data, err2 := proto.Marshal(res); err2 == nil {
						return res, data
					} else {
						return nil, []byte{}
					}
				} else {
					return nil, []byte{}
				}
			}).(*spb.RaftGroup)
		}
		if group != nil {
			group.Term = lastEntry.Term
			s.SetGroupLogLastIndex(ALPHA_GROUP, lastEntry.Index)
			// the index will be used to observe changes
			s.SaveGroup(group)
			for _, peer := range peers {
				s.SavePeer(peer)
			}

		}
	}
}