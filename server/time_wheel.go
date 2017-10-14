package server

import (
	"context"
	"math/rand"
	"time"
	"log"
)

func RandomTimeout(mult float32) int {
	lowRange := 1000 * mult
	highRange := 5000 * mult
	return int(lowRange + highRange*rand.Float32())
}

func RefreshTimer(meta *RTGroupMeta, mult float32) {
	meta.Timeout = time.Now().Add(time.Duration(RandomTimeout(mult)) * time.Millisecond)
}

func (s *BFTRaftServer) StartTimingWheel() {
	go func() {
		for true {
			for _, meta := range s.GroupsOnboard {
				if meta.Timeout.Before(time.Now()) {
					meta.Lock.Lock()
					if meta.Role == FOLLOWER {
						if meta.Leader == meta.Peer {
							panic("Follower is leader")
						}
						// not leader
						log.Println(s.Id, "is candidate")
						s.BecomeCandidate(meta)
					} else if meta.Role == LEADER {
						// is leader, send heartbeat
						s.SendFollowersHeartbeat(context.Background(), meta.Peer, meta.Group)
					} else if meta.Role == CANDIDATE {
						// is candidate but vote expired, start a new vote term
						log.Println(s.Id, "started a new election")
						s.BecomeCandidate(meta)
					} else if meta.Role == OBSERVER {
						// update local data
						s.PullAndCommitGroupLogs(meta.Group.Id)
						RefreshTimer(meta, 5)
					}
					meta.Lock.Unlock()
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}
