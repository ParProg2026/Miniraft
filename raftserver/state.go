package main

import (
	"log"
	"os"
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
type RaftServer struct {
	Identity         string
	Peers            []string
	Network          *NetworkManager
	State            ServerState
	CurrentTerm      int
	VotedFor         string
	VotesReceived    int
	ElectionDeadline time.Time
	Log              []miniraft.LogEntry
	CommitIndex      int
	LastApplied      int
	NextIndex        []int
	MatchIndex       []int
	IsSuspended      bool
	LeaderId         string
	logFile          *os.File
}

type ClientCommand struct {
	Command string
}

func (s *RaftServer) becomeFollower(term int) {
	s.State = Follower
	s.CurrentTerm = term
	s.VotedFor = ""
	s.VotesReceived = 0
	s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
}

func (s *RaftServer) becomeCandidate() {
	s.State = Candidate
	s.CurrentTerm++
	s.VotedFor = s.Identity
	s.VotesReceived = 1
}

func (s *RaftServer) becomeLeader() {
	s.State = Leader
	s.NextIndex = make([]int, len(s.Peers))
	s.MatchIndex = make([]int, len(s.Peers))

	for i := range s.Peers {
		s.NextIndex[i] = len(s.Log) + 1
		s.MatchIndex[i] = 0
	}
	if DEBUG {
		log.Printf("Yipee")
	}
}

func (s *RaftServer) HandleIncomingMessage(packet *IncomingPacket) {
	if s.IsSuspended {
		return
	}

	switch packet.Type {
	case IncomingClientCommand:
		cmd, ok := packet.Payload.(*ClientCommand)
		if !ok {
			log.Printf("bad payload type for client command from %s: %T", packet.From.String(), packet.Payload)
			return
		}
		s.handleClientCommand(cmd)

	case IncomingAppendEntriesRequest:
		req, ok := packet.Payload.(*miniraft.AppendEntriesRequest)
		if !ok {
			log.Printf("bad payload type for AppendEntriesRequest from %s: %T", packet.From.String(), packet.Payload)
			return
		}
		s.handleAppendEntriesRequest(req)

	case IncomingAppendEntriesResponse:
		resp, ok := packet.Payload.(*miniraft.AppendEntriesResponse)
		if !ok {
			log.Printf("bad payload type for AppendEntriesResponse from %s: %T", packet.From.String(), packet.Payload)
			return
		}
		s.handleAppendEntriesResponse(packet.From, resp)

	case IncomingRequestVoteRequest:
		req, ok := packet.Payload.(*miniraft.RequestVoteRequest)
		if !ok {
			log.Printf("bad payload type for RequestVoteRequest from %s: %T", packet.From.String(), packet.Payload)
			return
		}
		s.handleRequestVoteRequest(&packet.From, req)

	case IncomingRequestVoteResponse:
		resp, ok := packet.Payload.(*miniraft.RequestVoteResponse)
		if !ok {
			log.Printf("bad payload type for RequestVoteResponse from %s: %T", packet.From.String(), packet.Payload)
			return
		}
		s.handleRequestVoteResponse(resp)

	default:
		log.Printf("unknown packet kind from %s", packet.From.String())
	}
}
