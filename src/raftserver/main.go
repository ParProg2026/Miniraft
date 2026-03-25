// Package main is the entry point for the Raft server implementation.
// It initializes the networking, the state machine, and runs the central event loop.
// Usage: go run . <server-host:server-port> <filename>
package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	miniraft "raft/protocol"
	"time"
)

// main is the primary entry point. It orchestrates the setup of the Raft node
// and initiates the single-threaded event loop to guarantee thread safety.
func main() {
	// 1. Parse Command Line Arguments
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <server-host:server-port> <filename>\n", os.Args[0])
		os.Exit(1)
	}
	identity := os.Args[1]
	peersFile := os.Args[2]

	fmt.Printf("Starting Raft server %s using peers config %s...\n", identity, peersFile)

	// 2. Parse Cluster Configuration
	peers := readPeersConfig(peersFile, identity)
	fmt.Printf("Discovered peers: %v\n", peers)

	// 3. Initialize Network Infrastructure
	network := &NetworkManager{
		Peers: peers,
	}
	if err := network.InitListener(identity); err != nil {
		log.Fatalf("Failed to initialize network listener: %v", err)
	}
	defer network.Conn.Close()

	// 4. Initialize Persistent Storage
	logFile, err := os.OpenFile(identity+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open persistent log file: %v", err)
	}
	defer logFile.Close()

	// 5. Initialize Core State Machine with Dependency Injection
	state := RaftServer{
		Identity:         identity,
		Peers:            peers,
		Network:          network,
		State:            Follower,
		CurrentTerm:      0,
		VotedFor:         "",
		VotesReceived:    0,
		ElectionDeadline: time.Now().Add(randomElectionTimeout()),
		Log:              []miniraft.LogEntry{},
		CommitIndex:      0,
		LastApplied:      0,
		NextIndex:        []int{},
		MatchIndex:       []int{},
		IsSuspended:      false,
		logFile:          *logFile,
	}

	// 6. Setup Concurrency Channels for the Event Loop
	packetCh := make(chan *IncomingPacket, 100)
	cliCh := make(chan string, 10)
	
	// High-resolution clock tick for evaluating timeouts
	ticker := time.NewTicker(10 * time.Millisecond) 
	defer ticker.Stop()

	// Standard Raft heartbeat interval
	heartbeatTicker := time.NewTicker(100 * time.Millisecond)
	defer heartbeatTicker.Stop()

	// 7. Start Background Producers
	
	// Network Producer
	packetHandler := func(packet *IncomingPacket) {
		packetCh <- packet
	}
	go network.ListenLoop(packetHandler)

	// CLI Producer
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			cliCh <- scanner.Text()
		}
	}()

	// 8. The Central Event Loop (Actor Model)
	// This select block guarantees that the RaftServer state is only ever mutated
	// by one event at a time, completely eliminating memory race conditions.
	log.Println("Node is online and entering event loop.")
	for {
		select {
		case packet := <-packetCh:
			// Process incoming UDP network packets
			state.HandleIncomingMessage(packet)

		case cmd := <-cliCh:
			// Process local developer CLI commands
			processDebugCommand(&state, cmd)

		case <-heartbeatTicker.C:
			// Periodically broadcast heartbeats to maintain leadership authority
			// and replicate logs to followers.
			if state.State == Leader && !state.IsSuspended {
				state.sendHeartbeats()
			}

		case <-ticker.C:
			// Centralized Time Management
			if state.IsSuspended {
				continue
			}
			
			// Trigger elections if a Follower or Candidate times out
			if state.State != Leader && time.Now().After(state.ElectionDeadline) {
				state.startElection()
			}
		}
	}
}

// readPeersConfig reads the cluster configuration file and returns a list of peer addresses.
// It filters out the node's own identity to prevent the server from sending messages to itself.
func readPeersConfig(filename string, selfIdentity string) []string {
	var peers []string
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Failed to open peers file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		peer := scanner.Text()
		if peer != selfIdentity && peer != "" {
			peers = append(peers, peer)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading peers file: %v", err)
	}

	return peers
}

// processDebugCommand handles introspection and state control commands from standard input.
// It acts directly on the RaftServer memory space, which is safe because it is called
// synchronously from the central event loop.
func processDebugCommand(state *RaftServer, cmd string) {
	switch cmd {
	case "log":
		fmt.Printf("Current Log: %+v\n", state.Log)
	case "print":
		fmt.Printf("\n--- Node State ---\n")
		fmt.Printf("Identity:      %s\n", state.Identity)
		fmt.Printf("State:         %s\n", state.State)
		fmt.Printf("Term:          %d\n", state.CurrentTerm)
		fmt.Printf("VotedFor:      %q\n", state.VotedFor)
		fmt.Printf("VotesRx:       %d\n", state.VotesReceived)
		fmt.Printf("Log Length:    %d\n", len(state.Log))
		fmt.Printf("CommitIndex:   %d\n", state.CommitIndex)
		fmt.Printf("LastApplied:   %d\n", state.LastApplied)
		if state.State == Leader {
			fmt.Printf("NextIndex:     %v\n", state.NextIndex)
			fmt.Printf("MatchIndex:    %v\n", state.MatchIndex)
		}
		fmt.Printf("------------------\n\n")
	case "suspend":
		if !state.IsSuspended {
			state.IsSuspended = true
			fmt.Println("Server suspended (simulating crash)")
		} else {
			fmt.Println("Server is already suspended")
		}
	case "resume":
		if state.IsSuspended {
			state.IsSuspended = false
			// Reset the election deadline so it doesn't instantly panic/elect upon waking up
			state.ElectionDeadline = time.Now().Add(time.Duration(150) * time.Millisecond)
			fmt.Println("Server resumed")
		} else {
			fmt.Println("Server is already running")
		}
	default:
		fmt.Println("Unknown command. Valid options: log, print, suspend, resume")
	}
}