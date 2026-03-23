package main

import (
	"math/rand"
	"net"
	miniraft "raft/protocol"
	"time"
)

// handleAppendEntriesRequest processes an incoming AppendEntries RPC from a leader.
// As per Raft (Figure 2 in raft.pdf / OngaroPhD.pdf):
//  1. If request.Term < s.CurrentTerm, reply false immediately (reject stale leader).
//  2. If the request.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and become a Follower.
//     Also, receiving this RPC (if term >= CurrentTerm) should reset the election timer.
//  3. Reply false if the local log doesn't contain an entry at request.PrevLogIndex
//     whose term matches request.PrevLogTerm (Log Matching Property).
//  4. If an existing log entry conflicts with a new one (same index but different terms),
//     delete the existing entry and all that follow it.
//  5. Append any new entries not already in the log.
//  6. If request.LeaderCommit > s.CommitIndex, set s.CommitIndex = min(request.LeaderCommit, index of last new entry).
//  7. Finally, send an AppendEntriesResponse back to the leader (request.LeaderId) indicating success or failure.
//
// sendRaftMessage resolves the UDP address and sends a Raft message logic to the peer.
func (s *RaftServer) sendRaftMessage(peerAddr string, payload any) {
	msg := &miniraft.RaftMessage{Message: payload}
	data, err := msg.MarshalJson()
	if err != nil {
		return
	}
	addr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.Write(data)
}

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

	// Receiving this RPC (if term >= CurrentTerm) should reset the election timer.
	s.ElectionTimer.Reset(randomElectionTimeout())

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
				// Conflict: truncate the log at this index
				s.Log = append([]miniraft.LogEntry(nil), s.Log[:logIdx-1]...) // safe truncate
				s.Log = append(s.Log, entry)
			}
		} else {
			// Append new entry
			s.Log = append(s.Log, entry)
		}
	}

	// 6. If request.LeaderCommit > s.CommitIndex, set s.CommitIndex = min(request.LeaderCommit, index of last new entry).
	if request.LeaderCommit > s.CommitIndex {
		lastNewEntryIdx := request.PrevLogIndex + len(request.LogEntries)
		s.CommitIndex = min(request.LeaderCommit, lastNewEntryIdx)
	}

	response.Success = true
	s.sendRaftMessage(request.LeaderId, response)
}

// handleAppendEntriesResponse processes a response to an AppendEntries RPC sent by this node (Leader).
// As per Raft (Figure 2 in raft.pdf / OngaroPhD.pdf):
// 1. If response.Term > s.CurrentTerm, update s.CurrentTerm, clear s.VotedFor, and immediately transition to Follower.
// 2. If we are no longer the Leader, ignore the response.
// 3. If response.Success is true:
//   - Update the nextIndex and matchIndex for the follower that sent the response.
//   - Check if there exists an N > s.CommitIndex such that a majority of matchIndex[i] >= N,
//     and s.Log[N-1].Term == s.CurrentTerm (assuming 1-based indexing for logs).
//     If so, set s.CommitIndex = N and apply the newly committed log entries to the state machine.
//
// 4. If response.Success is false (due to log inconsistency):
//   - Decrement nextIndex for that follower.
//   - Retry the AppendEntries RPC with the new nextIndex and the corresponding entries.
func (s *RaftServer) handleAppendEntriesResponse(response *miniraft.AppendEntriesResponse) {

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
func (s *RaftServer) handleRequestVoteRequest(request *miniraft.RequestVoteRequest) {

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

}

// TODO: Implement Log Appending logic that writes to the persistent log file (e.g. `127-0-0-1-2000.log`).
func (s *RaftServer) sendAppendEntries(peer string) {
	// TODO: Implement AppendEntries RPC
}

// randomElectionTimeout generates a random duration for the election timeout,
// typically between 150ms and 300ms as recommended by the Raft paper.
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
