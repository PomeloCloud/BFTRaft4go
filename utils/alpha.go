package utils

import (
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"context"
	"github.com/PomeloCloud/BFTRaft4go/server"
)

func AlphaNodes(servers []string) []*spb.Node {
	bootstrapServers := []spb.BFTRaftClient{}
	for _, addr := range servers {
		if c, err := GetClusterRPC(addr); err == nil {
			bootstrapServers = append(bootstrapServers, c)
		}
	}
	return MajorityResponse(bootstrapServers, func(c spb.BFTRaftClient) (interface{}, []byte) {
		if nodes, err := c.AlphaNodes(context.Background(), &spb.Nothing{}); err == nil {
			return nodes, server.NodesSignData(nodes.Nodes)
		} else {
			return nil, []byte{}
		}
	}).(*spb.AlphaNodesResponse).Nodes
}