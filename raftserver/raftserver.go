package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	miniraft "raft/protocol"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <server-host:server-port> <filename>\n", os.Args[0])
		os.Exit(1)
	}
	identity := os.Args[1]
	peersFile := os.Args[2]

	//var hash int64
	//for _, c := range identity {
	//	hash += int64(c)
	//}
	//rand.Seed(time.Now().UnixNano() + hash)

	fmt.Printf("Starting Raft server %s using peers config %s...\n", identity, peersFile)

	peers := readPeersConfig(peersFile, identity)
	fmt.Printf("Discovered peers: %v\n", peers)

	network := &NetworkManager{
		Peers: peers,
	}
	if err := network.InitListener(identity); err != nil {
		log.Fatalf("Failed to initialize network listener: %v", err)
	}
	defer network.Conn.Close()

	logFile, err := os.OpenFile(identity+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open persistent log file: %v", err)
	}
	defer logFile.Close()

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
		logFile:          logFile,
	}

	packetCh := make(chan *IncomingPacket, 100)
	cliCh := make(chan string, 10)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	heartbeatTicker := time.NewTicker(100 * time.Millisecond)
	defer heartbeatTicker.Stop()

	packetHandler := func(packet *IncomingPacket) {
		packetCh <- packet
	}
	go network.ListenLoop(packetHandler)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			cliCh <- scanner.Text()
		}
	}()

	log.Println("Node is online and entering event loop.")
	for {
		select {
		case packet := <-packetCh:
			state.HandleIncomingMessage(packet)

		case cmd := <-cliCh:
			processDebugCommand(&state, cmd)

		case <-heartbeatTicker.C:
			if state.State == Leader && !state.IsSuspended {
				state.sendHeartbeats()
			}

		case <-ticker.C:
			if state.IsSuspended {
				continue
			}

			if state.State != Leader && time.Now().After(state.ElectionDeadline) {
				state.startElection()
			}
		}
	}
}

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
	return peers
}

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
			state.ElectionDeadline = time.Now().Add(randomElectionTimeout())
			fmt.Println("Server resumed")
		} else {
			fmt.Println("Server is already running")
		}
	default:
		fmt.Println("Unknown command. Valid options: log, print, suspend, resume")
	}
}
