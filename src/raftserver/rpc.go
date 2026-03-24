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
// Using this prevents the system from opening redundant UDP sockets on every request.
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

	// 1. If request.Term < s.CurrentTerm, reply false immediately (reject stale leader).
	if request.Term < s.CurrentTerm {
		s.sendRaftMessage(request.LeaderId, response)
		return
	}

	// 2. If the request.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and become a Follower.
	if request.Term > s.CurrentTerm || s.State != Follower {
		s.becomeFollower(request.Term)
		response.Term = s.CurrentTerm
	}

	// Safely reset the election timer if it exists.
	if s.ElectionTimer != nil {
		s.ElectionTimer.Reset(randomElectionTimeout())
	}

	// 3. Reply false if the local log doesn't contain an entry at request.PrevLogIndex
	//    whose term matches request.PrevLogTerm. (Using 1-based log indexing logically)
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

	// 4. If an existing log entry conflicts with a new one (same index but different terms),
	//    delete the existing entry and all that follow it.
	// 5. Append any new entries not already in the log.
	for i, entry := range request.LogEntries {
		logIdx := request.PrevLogIndex + 1 + i // Logical 1-based index
		if logIdx <= len(s.Log) {
			if s.Log[logIdx-1].Term != entry.Term {
				// Conflict: truncate the log safely
				s.Log = append([]miniraft.LogEntry(nil), s.Log[:logIdx-1]...)
				s.Log = append(s.Log, entry)
			}
		} else {
			// Append new entry
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

		// Append the entry to all peers
		for i, peer := range s.Peers {
			s.sendAppendEntries(i, peer)
		}
	}

	if s.State == Follower {
		// Forwards the command to the leader
		s.sendClientCommand(cmd)
	}
}

// handleAppendEntriesResponse processes a response to an AppendEntries RPC sent by this node (Leader).
func (s *RaftServer) handleAppendEntriesResponse(from net.UDPAddr, response *miniraft.AppendEntriesResponse) {
	// As per Raft (Figure 2 in raft.pdf / OngaroPhD.pdf):
	// 1. If response.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and immediately transition to Follower.
	if response.Term > s.CurrentTerm {
		s.becomeFollower(response.Term)
		return
	}

	// 2. If we are no longer the Leader, ignore the response.
	if s.State != Leader {
		return
	}

	var fromId = 0
	for i, peer := range s.Peers {
		if peer == from.String() {
			fromId = i
		}
	}

	// 3. If response.Success is true:
	//   - Update the nextIndex and matchIndex for the follower that sent the response.
	//   - Check if there exists an N > s.CommitIndex such that a majority of matchIndex[i] >= N,
	//     and s.Log[N-1].Term == s.CurrentTerm (assuming 1-based indexing for logs).
	//     If so, set s.CommitIndex = N and apply the newly committed log entries to the state machine.
	// 4. If response.Success is false (due to log inconsistency):
	//   - Decrement nextIndex for that follower.
	//   - Retry the AppendEntries RPC with the new nextIndex and the corresponding entries.
	if response.Success {
		s.NextIndex[fromId]++
		s.MatchIndex[fromId]++

		// check if we can commit something
		indices := make([]int, len(s.MatchIndex))
		copy(s.MatchIndex, indices)
		sort.Ints(indices)
		maxMaj := indices[(len(indices)-1)/2]
		if maxMaj > s.CommitIndex && s.Log[maxMaj-1].Term == s.CurrentTerm {
			for i := s.CommitIndex + 1; i <= maxMaj; i++ {
				// write out to log file: "term,index,command"
				_, err := s.logFile.WriteString(fmt.Sprintf("%d,%d,%s\n", s.Log[i-1].Term, i, s.Log[i-1].CommandName))
				if err != nil {
					log.Fatalf("Failed to write to log file: %v", err)
				}
			}

			s.CommitIndex = maxMaj
		}

		if s.NextIndex[fromId] < len(s.Log) {
			s.sendAppendEntries(fromId, from.String())
		}
	} else {
		s.NextIndex[fromId]--
		s.sendAppendEntries(fromId, from.String())
	}
}

// handleRequestVoteRequest processes an incoming RequestVote RPC from a Candidate.
// As per Raft (Figure 2 in raft.pdf / OngaroPhD.pdf):
//  1. If request.Term < s.CurrentTerm, reply false (reject stale candidate).
//  2. If request.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and step down to Follower.
//  3. If s.VotedFor is null (empty) or matches request.CandidateName, check if the candidate's log
//     is at least as up-to-date as our local log.
//     - Up-to-date check: If logs have last entries with different terms, the log with the later term is more up-to-date.
//     - If logs end with the same term, whichever log is longer is more up-to-date.
//  4. If the candidate's log is up-to-date, grant the vote (set s.VotedFor = request.CandidateName, reply true)
//     and reset the election timer.
//  5. Otherwise, do not grant the vote (reply false).
//  6. Send the RequestVoteResponse back to the candidate.
func (s *RaftServer) handleRequestVoteRequest(from *net.UDPAddr, request *miniraft.RequestVoteRequest) {
	// TODO: Implement logic to grant or deny vote based on Term and log freshness.
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

	if s.VotedFor == "" || s.VotedFor == request.CandidateName {
		s.VotedFor = request.CandidateName
		resp := &miniraft.RequestVoteResponse{
			Term:        s.CurrentTerm,
			VoteGranted: true,
		}

		s.sendRaftMessage(from.String(), resp)
		log.Printf("Voting for %s", request.CandidateName)
		return
	}

	s.sendRaftMessage(from.String(), resp)
}

// handleRequestVoteResponse processes a response to a RequestVote RPC sent by this node (Candidate).
// As per Raft (Figure 2 in raft.pdf / OngaroPhD.pdf):
//  1. If response.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and immediately transition to Follower.
//  2. If we are no longer a Candidate (e.g., we stepped down or already became Leader), ignore the response.
//  3. If response.VoteGranted is true, increment s.VotesReceived.
//  4. If s.VotesReceived reaches a majority of the cluster (i.e. > len(TotalPeers)/2),
//     transition to Leader utilizing the becomeLeader() function.
//     - Upon becoming Leader, immediately send empty AppendEntries RPCs (heartbeats) to all peers
//     to establish authority and prevent new elections.
func (s *RaftServer) handleRequestVoteResponse(response *miniraft.RequestVoteResponse) {
	// TODO: Implement logic to tally votes and transition to Leader if majority reached.
	if response.Term > s.CurrentTerm {
		s.becomeFollower(response.Term)
	}

	if s.State != Candidate {
		return
	}

	if response.VoteGranted {
		s.VotesReceived++
	}

	if s.VotesReceived > len(s.Peers)/2 {
		s.becomeLeader()
		go s.sendHeartbeats()
	}
}

// sendAppendEntries builds and dispatches an AppendEntries Request to a specific peer.
func (s *RaftServer) sendAppendEntries(index int, peer string) {
	prevLogIndex := s.NextIndex[index]

	s.sendRaftMessage(peer, &miniraft.AppendEntriesRequest{
		Term:         s.CurrentTerm,
		LeaderId:     s.Identity,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  s.Log[prevLogIndex].Term,
		LeaderCommit: s.CommitIndex,
		LogEntries:   s.Log[prevLogIndex : prevLogIndex+1],
	})
}

func (s *RaftServer) sendClientCommand(cmd *ClientCommand) {
	if s.State == Follower {
		s.sendRaftMessage(s.LeaderId, cmd)
	}
}

// randomElectionTimeout generates a randomized duration between 150ms and 300ms.
// The randomness is crucial to prevent split votes during leader elections.
func randomElectionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond
}
