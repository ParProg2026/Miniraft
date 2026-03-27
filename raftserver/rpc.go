// Package main implements the RPC handlers required for the Raft consensus protocol.
// It manages all log replication, majority calculations, and persistent state flushing.
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

// sendRaftMessage is a helper function that leverages the injected NetworkManager
// to safely transmit serialized payloads to the specified peer.
func (s *RaftServer) sendRaftMessage(peerAddr string, payload any) {
	err := s.Network.SendRaftMessage(peerAddr, payload)
	if err != nil {
		log.Printf("Failed to send RPC to %s: %v\n", peerAddr, err)
	}
}

func (s *RaftServer) writeOutCommits(newCommitIndex int) {
	for i := s.CommitIndex + 1; i <= newCommitIndex; i++ {
		_, err := fmt.Fprintf(s.logFile, "%d,%d,%s\n", s.Log[i-1].Term, i, s.Log[i-1].CommandName)
		if err != nil {
			log.Printf("Failed to write to log file: %v", err)
		}
	}
	err := s.logFile.Sync() // Force an OS-level flush to disk
	if err != nil {
		log.Printf("Failed to sync log file: %v", err)
		return
	}
	s.CommitIndex = newCommitIndex
}

// handleAppendEntriesRequest processes an incoming AppendEntries RPC from an active leader.
// It handles term synchronization, log truncation for conflicts, and persistent disk flushing.
func (s *RaftServer) handleAppendEntriesRequest(request *miniraft.AppendEntriesRequest) {
	response := &miniraft.AppendEntriesResponse{
		Term:    s.CurrentTerm,
		Success: false,
	}

	// 1. Reject stale leaders immediately to protect cluster integrity.
	if request.Term < s.CurrentTerm {
		s.sendRaftMessage(request.LeaderId, response)
		return
	}

	// 2. Acknowledge newer terms or transition back to a Follower state.
	if request.Term > s.CurrentTerm || s.State != Follower {
		s.becomeFollower(request.Term)
		response.Term = s.CurrentTerm
	}

	// Safely reset the election timer to acknowledge the leader's authority and prevent split votes.
	s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
	s.LeaderId = request.LeaderId

	// 3. Reject the payload if the local log lacks an entry at PrevLogIndex matching PrevLogTerm.
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

	// 4 & 5. Append new entries and truncate any uncommitted conflicting logs.
	for i, entry := range request.LogEntries {
		logIdx := request.PrevLogIndex + 1 + i // Logical 1-based index
		if logIdx <= len(s.Log) {
			if s.Log[logIdx-1].Term != entry.Term {
				// Safely truncate the log slice to remove the conflict
				s.Log = append([]miniraft.LogEntry(nil), s.Log[:logIdx-1]...)
				s.Log = append(s.Log, entry)
			}
		} else {
			s.Log = append(s.Log, entry)
		}
	}

	// 6. Advance the CommitIndex and persist newly committed entries to disk.
	if request.LeaderCommit > s.CommitIndex {
		lastNewEntryIdx := request.PrevLogIndex + len(request.LogEntries)
		newCommitIndex := min(request.LeaderCommit, lastNewEntryIdx)

		// Persist the missing follower entries to the physical log file
		if newCommitIndex > s.CommitIndex {
			s.writeOutCommits(newCommitIndex)
		}
	}

	response.Success = true
	s.sendRaftMessage(request.LeaderId, response)
}

// handleClientCommand processes incoming commands from the external client application.
// Leaders append and replicate; Followers proxy the command to the active leader.
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

		// Immediately broadcast the new entry to minimize replication latency
		for i, peer := range s.Peers {
			s.sendAppendEntries(i, peer)
		}
	}

	if s.State == Follower {
		s.sendClientCommand(s.LeaderId, cmd)
	}
}

// handleAppendEntriesResponse processes a Follower's reply to a Leader's replication request.
// It manages the volatile MatchIndex tracking and calculates the global cluster commit consensus.
func (s *RaftServer) handleAppendEntriesResponse(from net.UDPAddr, response *miniraft.AppendEntriesResponse) {
	// Step down immediately if a newer term is discovered in the cluster
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
		// Update follower tracking matrices
		if s.NextIndex[fromId] <= len(s.Log) {
			s.MatchIndex[fromId] = s.NextIndex[fromId]
			s.NextIndex[fromId] = s.MatchIndex[fromId] + len(s.Log[s.NextIndex[fromId]-1:])
		}

		// Calculate cluster majority
		indices := make([]int, len(s.MatchIndex)+1)
		copy(indices, s.MatchIndex)
		indices[len(s.MatchIndex)] = len(s.Log) // Log of the leader
		sort.Ints(indices)

		// The median of the array representing all nodes determines the globally committed index
		maxMaj := indices[len(indices)/2]

		if maxMaj > s.CommitIndex && s.Log[maxMaj-1].Term == s.CurrentTerm {
			s.writeOutCommits(maxMaj)
		}

		// If the follower is still lagging after a successful append, send the next batch
		if s.NextIndex[fromId] <= len(s.Log) {
			s.sendAppendEntries(fromId, from.String())
		}
	} else {
		// Follower log is inconsistent, aggressively decrement nextIndex and retry the replication
		if s.NextIndex[fromId] > 1 {
			s.NextIndex[fromId]--
		}
		s.sendAppendEntries(fromId, from.String())
	}
}

// handleRequestVoteRequest processes an incoming RequestVote RPC from a Candidate node.
// It enforces strict Raft safety checks to guarantee a node with stale logs cannot win an election.
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

	// Grant the vote only if we haven't voted elsewhere and the candidate's log is sufficiently up-to-date
	if s.VotedFor == "" || s.VotedFor == request.CandidateName {
		if request.LastLogTerm > prevTerm || (request.LastLogTerm == prevTerm && request.LastLogIndex >= prevIndex) {
			s.VotedFor = request.CandidateName
			resp := &miniraft.RequestVoteResponse{
				Term:        s.CurrentTerm,
				VoteGranted: true,
			}

			s.sendRaftMessage(from.String(), resp)
			s.ElectionDeadline = time.Now().Add(randomElectionTimeout())
			if DEBUG {
				log.Printf("Voting for %s", request.CandidateName)
			}
			return
		}
	}
	s.sendRaftMessage(from.String(), resp)
}

// handleRequestVoteResponse processes incoming votes during an active election cycle.
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

	// Check for a strict majority rule (n/2 + 1)
	if s.VotesReceived > len(s.Peers)/2 {
		s.becomeLeader()
		// Synchronously send initial heartbeat to establish authority instantly and prevent timeouts.
		s.sendHeartbeats()
	}
}

// sendAppendEntries builds and dispatches an AppendEntries Request to a specific peer.
// It acts as the core mechanism for batch log replication.
// sendAppendEntries builds and dispatches an AppendEntries Request to a specific peer.
func (s *RaftServer) sendAppendEntries(index int, peer string) {
	prevLogIndex := s.NextIndex[index] - 1
	prevTerm := 0
	if prevLogIndex > 0 && prevLogIndex <= len(s.Log) {
		prevTerm = s.Log[prevLogIndex-1].Term
	}

	entries := make([]miniraft.LogEntry, 0)
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

// randomElectionTimeout generates a non-deterministic duration.
func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}

func (s *RaftServer) sendClientCommand(peer string, cmd *ClientCommand) {
	s.sendRaftMessage(peer, cmd)
}
