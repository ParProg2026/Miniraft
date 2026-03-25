package main

import (
	"log"
	miniraft "raft/protocol"
	"time"
)

func (s *RaftServer) startElection() {
	s.becomeCandidate()

	log.Printf("Starting election term: %d", s.CurrentTerm)
	prevIndex := len(s.Log)
	prevTerm := 0
	if prevIndex > 0 {
		prevTerm = s.Log[prevIndex-1].Term
	}

	req := &miniraft.RequestVoteRequest{
		Term:          s.CurrentTerm,
		CandidateName: s.Identity,
		LastLogIndex:  prevIndex,
		LastLogTerm:   prevTerm,
	}

	s.Network.BroadcastRaftMessage(req)
	s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
}

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
