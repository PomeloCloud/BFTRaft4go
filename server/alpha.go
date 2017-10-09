package server

import (
	"context"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
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
	alphaPeersRes := utils.MajorityResponse(alphaRPCs, func(client spb.BFTRaftClient) (interface{}, []byte) {
		if res, err := client.GroupPeers(context.Background(), &spb.GroupPeersRequest{
			GroupId: ALPHA_GROUP,
		}); err == nil {
			return res, server.PeersSignData(res.Peers)
		} else {
			return nil, []byte{}
		}
	}).(*spb.GroupPeersResponse)
	peers := alphaPeersRes.Peers
	lastLog := alphaPeersRes.LastLog

}

func (s *BFTRaftServer) AttachAlphaGroup() {
	// this function will either rejoin or observe alpha group
	if _, isMember := s.GroupsOnboard[ALPHA_GROUP]; isMember {
		// it is a member of alpha group
		// should been updated in a short time
	} else {

	}
}
