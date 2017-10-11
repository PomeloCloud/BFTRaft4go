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

func CommandSignData(group uint64, sender uint64, reqId uint64, data []byte) []byte {
	groupBytes := U64Bytes(group)
	senderBytes := U64Bytes(sender)
	reqIdBytes := U64Bytes(reqId)
	return append(append(append(groupBytes, senderBytes...), reqIdBytes...), data...)
}

func ExecCommandSignData(cmd *pb.CommandRequest) []byte {
	return CommandSignData(
		cmd.Group,
		cmd.ClientId,
		cmd.RequestId,
		append(U64Bytes(cmd.FuncId), cmd.Arg...),
	)
}