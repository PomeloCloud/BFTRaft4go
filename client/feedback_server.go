package client

import (
	"context"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
	"log"
)

type FeedbackServer struct {
	ClientIns *BFTRaftClient
}

func (fs *FeedbackServer) ResponseCommand(ctx context.Context, cmd *pb.CommandResult) (*pb.Nothing, error) {
	// TODO: Verify signature
	// signData := server.CommandSignData(cmd.Group, cmd.NodeId, cmd.RequestId, cmd.Result)
	log.Println("command response from:", cmd.NodeId, "for group:", cmd.Group, "reqId:", cmd.RequestId)
	go func() {
		fs.ClientIns.CmdResChan[cmd.Group][cmd.RequestId] <- cmd.Result
	}()
	return &pb.Nothing{}, nil
}

