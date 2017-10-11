package utils

import (
	"github.com/golang/protobuf/proto"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)

const (
	MAX_TERM_BUMP = 10
	ALPHA_GROUP   = 1 // the group for recording server members, groups, peers etc
)

func NodesSignData(nodes []*pb.Host) []byte {
	signData := []byte{}
	for _, node := range nodes {
		nodeBytes, _ := proto.Marshal(node)
		signData = append(signData, nodeBytes...)
	}
	return signData
}