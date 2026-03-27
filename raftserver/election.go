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

// sendHeartbeats builds and dispatches AppendEntries RPCs to all peers.
// It serves as both a keep-alive and the primary log replication mechanism.
// This function performs a single broadcast pass and yields control back to the event loop.
func (s *RaftServer) sendHeartbeats() {
	if s.State != Leader {
		return
	}

	for i, peer := range s.Peers {
		s.sendAppendEntries(i, peer)
	}
}
