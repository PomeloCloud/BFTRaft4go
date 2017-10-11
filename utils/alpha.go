package utils

import (
	"context"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)

func AlphaNodes(servers []string) []*spb.Host {
	bootstrapServers := []spb.BFTRaftClient{}
	for _, addr := range servers {
		if c, err := GetClusterRPC(addr); err == nil {
			bootstrapServers = append(bootstrapServers, c)
		}
	}
	return MajorityResponse(bootstrapServers, func(c spb.BFTRaftClient) (interface{}, []byte) {
		if nodes, err := c.GroupHosts(context.Background(), &spb.GroupId{
			GroupId: ALPHA_GROUP,
		}); err == nil {
			return nodes, NodesSignData(nodes.Nodes)
		} else {
			return nil, []byte{}
		}
	}).(*spb.GroupNodesResponse).Nodes
}
