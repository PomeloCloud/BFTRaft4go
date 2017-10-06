package server

import (
	"time"
	"math/rand"
	"context"
)

func RandomTimeout() int {
	lowRange := 50
	highRange := 500
	return lowRange + int(float32(highRange) * rand.Float32())
}

func (s *BFTRaftServer) StartTimingWheel() {
	go func() {
		for true {
			for groupId, meta := range s.GroupsOnboard {
				if meta.Timeout.After(time.Now()) {
					if meta.Leader != meta.Peer {
						// not leader
						// TODO: request votes
					} else {
						// is leader, send heartbeat
						s.SendFollowersHeartbeat(context.Background(), meta.Peer, meta.Group)
					}
				}
				meta.Timeout = time.Now().Add(time.Duration(RandomTimeout()) * time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}