package utils

import (
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
)


func GetClusterRPC(addr string) (spb.BFTRaftClient, error) {
	if cc, err := GetClientConn(addr); err == nil {
		return spb.NewBFTRaftClient(cc), nil
	} else {
		return nil, err
	}
}