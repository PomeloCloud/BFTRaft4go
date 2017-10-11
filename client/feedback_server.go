package client

import (
	"context"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/client"
)

type FeedbackServer struct {
	clientIns *BFTRaftClient
}

func (fs *FeedbackServer) ResponseCommand(ctx context.Context, cmd *pb.CommandResult) (*pb.Nothing, error) {
	// TODO: Verify signature
	// signData := server.CommandSignData(cmd.Group, cmd.NodeId, cmd.RequestId, cmd.Result)
	fs.clientIns.CmdResChan[cmd.Group][cmd.RequestId] <- cmd.Result
	return &pb.Nothing{}, nil
}

