// Package main implements the RPC handlers required for the Raft consensus protocol.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	miniraft "raft/protocol"
	"sort"
	"time"
)

// sendRaftMessage is a helper function that leverages the injected NetworkManager.
func (s *RaftServer) sendRaftMessage(peerAddr string, payload any) {
	err := s.Network.SendRaftMessage(peerAddr, payload)
	if err != nil {
		log.Printf("Failed to send RPC to %s: %v\n", peerAddr, err)
	}
}

// handleAppendEntriesRequest processes an incoming AppendEntries RPC from a leader.
func (s *RaftServer) handleAppendEntriesRequest(request *miniraft.AppendEntriesRequest) {
	response := &miniraft.AppendEntriesResponse{
		Term:    s.CurrentTerm,
		Success: false,
	}

	// 1. If request.Term < s.CurrentTerm, reply false immediately.
	if request.Term < s.CurrentTerm {
		s.sendRaftMessage(request.LeaderId, response)
		return
	}

	// 2. If the request.Term > s.CurrentTerm, become a Follower.
	if request.Term > s.CurrentTerm || s.State != Follower {
		s.becomeFollower(request.Term)
		response.Term = s.CurrentTerm
	}

	// Safely reset the election timer to acknowledge the leader's authority.
	s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
	s.LeaderId = request.LeaderId

	// 3. Reply false if the local log doesn't contain an entry at request.PrevLogIndex
	if request.PrevLogIndex > 0 {
		if request.PrevLogIndex > len(s.Log) {
			s.sendRaftMessage(request.LeaderId, response)
			return
		}
		if s.Log[request.PrevLogIndex-1].Term != request.PrevLogTerm {
			s.sendRaftMessage(request.LeaderId, response)
			return
		}
	}

	// 4 & 5. Append any new entries not already in the log.
	for i, entry := range request.LogEntries {
		logIdx := request.PrevLogIndex + 1 + i // Logical 1-based index
		if logIdx <= len(s.Log) {
			if s.Log[logIdx-1].Term != entry.Term {
				s.Log = append([]miniraft.LogEntry(nil), s.Log[:logIdx-1]...)
				s.Log = append(s.Log, entry)
			}
		} else {
			s.Log = append(s.Log, entry)
		}
	}

	// 6. If request.LeaderCommit > s.CommitIndex, advance the CommitIndex.
	if request.LeaderCommit > s.CommitIndex {
		lastNewEntryIdx := request.PrevLogIndex + len(request.LogEntries)
		s.CommitIndex = min(request.LeaderCommit, lastNewEntryIdx)
	}

	response.Success = true
	s.sendRaftMessage(request.LeaderId, response)
}

func (s *RaftServer) handleClientCommand(cmd *ClientCommand) {
	if s.IsSuspended {
		return
	}

	if s.State == Leader {
		entry := miniraft.LogEntry{
			Index:       len(s.Log) + 1,
			Term:        s.CurrentTerm,
			CommandName: cmd.Command,
		}
		s.Log = append(s.Log, entry)

		for i, peer := range s.Peers {
			s.sendAppendEntries(i, peer)
		}
	}

	if s.State == Follower {
		s.sendClientCommand(cmd)
	}
}

// handleAppendEntriesResponse processes a response to an AppendEntries RPC sent by this node (Leader).
func (s *RaftServer) handleAppendEntriesResponse(from net.UDPAddr, response *miniraft.AppendEntriesResponse) {
	if response.Term > s.CurrentTerm {
		s.becomeFollower(response.Term)
		return
	}

	if s.State != Leader {
		return
	}

	var fromId = -1
	for i, peer := range s.Peers {
		if peer == from.String() {
			fromId = i
		}
	}
	if fromId == -1 {
		return
	}

	if response.Success {
		// Update follower tracking
		if s.NextIndex[fromId] <= len(s.Log) {
			s.MatchIndex[fromId] = s.NextIndex[fromId]
			s.NextIndex[fromId] = s.MatchIndex[fromId] + len(s.Log[s.NextIndex[fromId]-1:])
		}

		// Calculate cluster majority. MUST include the Leader's own up-to-date log length.
		indices := make([]int, len(s.MatchIndex)+1)
		copy(indices, s.MatchIndex)
		indices[len(s.MatchIndex)] = len(s.Log) // The leader always has its own entries
		sort.Ints(indices)
		
		// The median of the array representing all nodes determines the globally committed index
		maxMaj := indices[len(indices)/2]
		
		if maxMaj > s.CommitIndex && s.Log[maxMaj-1].Term == s.CurrentTerm {
			for i := s.CommitIndex + 1; i <= maxMaj; i++ {
				// Safely write to disk
				_, err := s.logFile.WriteString(fmt.Sprintf("%d,%d,%s\n", s.Log[i-1].Term, i, s.Log[i-1].CommandName))
				if err != nil {
					log.Printf("Failed to write to log file: %v", err)
				}
				s.logFile.Sync() // Force flush to disk
			}
			s.CommitIndex = maxMaj
		}

		if s.NextIndex[fromId] <= len(s.Log) {
			s.sendAppendEntries(fromId, from.String())
		}
	} else {
		// Follower log is inconsistent, decrement nextIndex and retry
		if s.NextIndex[fromId] > 1 {
			s.NextIndex[fromId]--
		}
		s.sendAppendEntries(fromId, from.String())
	}
}

func (s *RaftServer) handleRequestVoteRequest(from *net.UDPAddr, request *miniraft.RequestVoteRequest) {
	resp := &miniraft.RequestVoteResponse{
		Term:        s.CurrentTerm,
		VoteGranted: false,
	}

	if request.Term < s.CurrentTerm {
		s.sendRaftMessage(from.String(), resp)
		return
	}

	if request.Term > s.CurrentTerm {
		s.becomeFollower(request.Term)
	}

	prevIndex := len(s.Log)
	prevTerm := 0
	if prevIndex > 0 {
		prevTerm = s.Log[prevIndex-1].Term
	}

	if s.VotedFor == "" || s.VotedFor == request.CandidateName {
		if request.LastLogTerm > prevTerm || (request.LastLogTerm == prevTerm && request.LastLogIndex >= prevIndex) {
			s.VotedFor = request.CandidateName
			resp := &miniraft.RequestVoteResponse{
				Term:        s.CurrentTerm,
				VoteGranted: true,
			}

			s.sendRaftMessage(from.String(), resp)
			s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
			log.Printf("Voting for %s", request.CandidateName)
			return
		}
	}
	s.sendRaftMessage(from.String(), resp)
}

func (s *RaftServer) handleRequestVoteResponse(response *miniraft.RequestVoteResponse) {
	if response.Term > s.CurrentTerm {
		s.becomeFollower(response.Term)
		return
	}

	if s.State != Candidate {
		return
	}

	if response.VoteGranted {
		s.VotesReceived++
	}

	if s.VotesReceived > len(s.Peers)/2 {
		s.becomeLeader()
		// Send initial heartbeat to establish authority instantly.
		// Note: We call it synchronously; main.go will handle periodic ticks.
		s.sendHeartbeats()
	}
}

// sendAppendEntries builds and dispatches an AppendEntries Request to a specific peer.
func (s *RaftServer) sendAppendEntries(index int, peer string) {
	prevLogIndex := s.NextIndex[index] - 1
	prevTerm := 0
	if prevLogIndex > 0 && prevLogIndex <= len(s.Log) {
		prevTerm = s.Log[prevLogIndex-1].Term
	}

	// Slice from NextIndex to the end of the log to batch replicate entries
	var entries []miniraft.LogEntry
	if len(s.Log) >= s.NextIndex[index] {
		entries = s.Log[s.NextIndex[index]-1:]
	}

	s.sendRaftMessage(peer, &miniraft.AppendEntriesRequest{
		Term:         s.CurrentTerm,
		LeaderId:     s.Identity,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevTerm,
		LeaderCommit: s.CommitIndex,
		LogEntries:   entries,
	})
}

func (s *RaftServer) sendClientCommand(cmd *ClientCommand) {
	if s.State == Follower && s.LeaderId != "" {
		s.sendRaftMessage(s.LeaderId, cmd)
	}
}

func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}

// min returns the smaller of a or b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}