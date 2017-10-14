package utils

import (
	"context"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"log"
)

func AlphaNodes(servers []string) []*spb.Host {
	bootstrapServers := []*spb.BFTRaftClient{}
	for _, addr := range servers {
		if c, err := GetClusterRPC(addr); err == nil {
			bootstrapServers = append(bootstrapServers, &c)
		}
	}
	res := MajorityResponse(bootstrapServers, func(c spb.BFTRaftClient) (interface{}, []byte) {
		if nodes, err := c.GroupHosts(context.Background(), &spb.GroupId{
			GroupId: ALPHA_GROUP,
		}); err == nil {
			return nodes, NodesSignData(nodes.Nodes)
		} else {
			log.Println("cannot get alpha group hosts when get alpha nodes")
			return (*spb.GroupNodesResponse)(nil), []byte{}
		}
	})
	if res == nil {
		return nil
	} else {
		response := res.(*spb.GroupNodesResponse)
		return response.Nodes
	}
}
