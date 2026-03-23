package main

import (
	"net"
	miniraft "raft/protocol"
	"time"
)

// ServerState represents the 4 possible roles of a Raft node.
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

// RaftServer encapsulates the full consensus state of a single Raft node.
// It relies on the NetworkManager for all UDP I/O operations.
type RaftServer struct {
	Identity      string
	Peers         []string
	Network       *NetworkManager // Dependency injected for handling all I/O
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

// becomeFollower transitions the server to the Follower state and updates the current term.
// This is triggered when a node discovers a leader or candidate with a higher term.
func (s *RaftServer) becomeFollower(term int) {
	s.State = Follower
	s.CurrentTerm = term // Update term to the one of the leader
	s.VotedFor = ""      // Reset vote
	s.VotesReceived = 0  // Reset vote counter
}

// becomeCandidate transitions the server to the Candidate state to initiate an election.
// It increments the current term and votes for itself.
func (s *RaftServer) becomeCandidate() {
	s.State = Candidate
	s.CurrentTerm++         // Candidate for the new term
	s.VotedFor = s.Identity // Votes for itself
	s.VotesReceived = 1
}

// becomeLeader transitions a winning candidate to the Leader state.
// It initializes the volatile leader tracking structures (NextIndex and MatchIndex) for log replication.
func (s *RaftServer) becomeLeader() {
	s.State = Leader

	s.NextIndex = make([]int, len(s.Peers))  // Initialized to leader last log index + 1
	s.MatchIndex = make([]int, len(s.Peers)) // Initialized to 0

	for i := range s.Peers {
		s.NextIndex[i] = len(s.Log) + 1
		s.MatchIndex[i] = 0
	}
}

// HandleIncomingMessage is the central dispatcher for all network traffic.
// It routes parsed JSON messages from the network layer to the appropriate RPC handler.
func (s *RaftServer) HandleIncomingMessage(addr net.Addr, msgType miniraft.MessageType, payload any) {
	if s.IsSuspended {
		return // Silently drop packets if node is simulating a crash
	}

	switch msgType {
	case miniraft.AppendEntriesRequestMessage:
		s.handleAppendEntriesRequest(payload.(*miniraft.AppendEntriesRequest))
	case miniraft.AppendEntriesResponseMessage:
		s.handleAppendEntriesResponse(payload.(*miniraft.AppendEntriesResponse))
	case miniraft.RequestVoteRequestMessage:
		s.handleRequestVoteRequest(payload.(*miniraft.RequestVoteRequest))
	case miniraft.RequestVoteResponseMessage:
		s.handleRequestVoteResponse(payload.(*miniraft.RequestVoteResponse))
	}
}
