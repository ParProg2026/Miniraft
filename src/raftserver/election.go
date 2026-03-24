package main

import (
	miniraft "raft/protocol"
	"time"
)

// Requires election before testing
func (s *RaftServer) sendHeartbeats() {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if s.State != Leader {
			return
		}
		for i, peer := range s.Peers {
			prevIndex := s.NextIndex[i] - 1
			prevTerm := 0
			if prevIndex > 0 {
				prevTerm = s.Log[prevIndex-1].Term
			}

			req := &miniraft.AppendEntriesRequest{
				Term:         s.CurrentTerm,
				PrevLogIndex: prevIndex,
				PrevLogTerm:  prevTerm,
				LeaderCommit: s.CommitIndex,
				LeaderId:     s.Identity,
				LogEntries:   nil,
			}
			s.Network.SendRaftMessage(peer, req)
		}
	}
}
