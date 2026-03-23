package main

import (
	miniraft "raft/protocol"
	"time"
)

// ServerState represents the 4 possible states of a Raft node.
type ServerState int

const (
	Follower ServerState = iota
	Candidate
	Leader
	Failed
)

func (s ServerState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	case Failed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// RaftServer contains the full state of a single Raft node, as well as networking
// components and configurations necessary for the consensus algorithm.
type RaftServer struct {
	Identity      string
	Peers         []string
	State         ServerState
	CurrentTerm   int
	VotedFor      string
	VotesReceived int
	ElectionTimer *time.Timer
	Log           []miniraft.LogEntry
	CommitIndex   int
	LastApplied   int
	NextIndex     []int
	MatchIndex    []int
	IsSuspended   bool
}

// Transition to Follower state
func (s *RaftServer) becomeFollower(term int) {
	s.State = Follower
	s.CurrentTerm = term // Update term to the one of the leader
	s.VotedFor = ""      // Did not vote
	s.VotesReceived = 0  // Reset vote counter
}

// Transition to Candidate state
func (s *RaftServer) becomeCandidate() {
	s.State = Candidate
	s.CurrentTerm++ // Candidate for the new term
	// Votes for itself
	s.VotedFor = s.Identity
	s.VotesReceived = 1
}

func (s *RaftServer) becomeLeader() {
	s.State = Leader

	s.NextIndex = make([]int, len(s.Peers))  // Initialized to leader last log index + 1
	s.MatchIndex = make([]int, len(s.Peers)) // Initialized to 0
	for i := range s.Peers {
		s.NextIndex[i] = len(s.Log) + 1
		s.MatchIndex[i] = 0
	}
}
