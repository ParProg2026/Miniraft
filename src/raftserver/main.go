package main

import(
	"os"
	"fmt"
	"log"
	"bufio"
	"net"
	miniraft "raft/protocol"

)

// Usage:
//
//	go run raftserver.go server-host:server-port filename
//
//	- `server-host:server-port`: the identity and listening address of this node.
//	- `filename`: path to a file containing identities of all nodes in the cluster.
//
// Behavior:
//   - Starts as a Follower.
//   - Listens on the specified UDP port for RPCs (`AppendEntriesRequest`, `RequestVoteRequest`) and client commands.
//   - Listens on standard input for debug commands (`log`, `print`, `suspend`, `resume`).
//   - Manages an election timer to transition to Candidate if no heartbeat is received from the leader.
//   - If Leader, sends periodic heartbeats and replicates logs to followers.

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <server-host:server-port> <filename>\n", os.Args[0])
		os.Exit(1)
	}

	identity := os.Args[1]
	peersFile := os.Args[2]

	fmt.Printf("Starting Raft server %s using peers config %s...\n", identity, peersFile)

	var peers []string
	file, err := os.Open(peersFile)

	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		peer := scanner.Text()
		if peer != identity {
			peers = append(peers, peer)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Println(peers)
	// Initialize the RaftServer struct.
	state := RaftServer{
		Identity:      identity,
		Peers:         peers,
		State:         Follower,
		CurrentTerm:   0,
		VotedFor:      "",
		VotesReceived: 0,
		Log:           []miniraft.LogEntry{},
		CommitIndex:   0,
		LastApplied:   0,
		NextIndex:     []int{},
		MatchIndex:    []int{},
	}
	// Open a log file for appending committed entries.
	logFile, err := os.OpenFile(identity+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()

	// TODO: Start the UDP listener for incoming Raft messages and client requests.
	//  - Ensure packet size is handled appropriately (< 1400 bytes).
	//  - Use miniraft.go structures and `RaftMessage.UnmarshalJSON`.

	addr, err := net.ResolveUDPAddr("udp", identity)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", addr)

	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Goroutine for reading debug commands from `stdin`.
	inputChan := make(chan string)
	go func() {
		// Read commands from stdin
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			inputChan <- scanner.Text()
		}
		close(inputChan)
	}()

	// Goroutine for reading UDP messages.
	type UDPMessage struct {
		n    int
		addr *net.UDPAddr
		data []byte
	}
	udpChan := make(chan UDPMessage)
	go func() {
		for {
			buffer := make([]byte, 1400) // Max UDP packet size
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Println("Error reading from UDP:", err)
				continue
			}
			udpChan <- UDPMessage{n: n, addr: addr, data: buffer}
		}
	}()

	// Main event loop / state machine loop
	// Runs on the main thread so that "main" never returns and exits the program.
	for {
		select {
		case debugCommand := <-inputChan:
			switch debugCommand {
			case "log":
				fmt.Println(state.Log)
			case "print":
				fmt.Printf("--- Node State ---\n")
				fmt.Printf("Identity:      %s\n", state.Identity)
				fmt.Printf("Peers:         %v\n", state.Peers)
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
				fmt.Printf("------------------\n")
			case "suspend":
				if !state.IsSuspended {
					state.IsSuspended = true
					fmt.Println("Server suspended")
				} else {
					fmt.Println("Server is already suspended")
				}
			case "resume":
				if state.IsSuspended {
					state.IsSuspended = false
					fmt.Println("Server resumed")
				} else {
					fmt.Println("Server is already running")
				}
			default:
				fmt.Println("Unknown command")
			}

		case msg := <-udpChan:
			fmt.Println("Received message from:", msg.addr)
			// Handle incoming Raft messages and client requests based on msg.data[:msg.n]
			if state.IsSuspended {
				continue
			}

			var message miniraft.RaftMessage
			msgType, err := message.UnmarshalJSON(msg.data[:msg.n])
			if err != nil {
				fmt.Println("Error unmarshalling message:", err)
				continue
			}

			switch msgType {
			case miniraft.AppendEntriesRequestMessage:
				state.handleAppendEntriesRequest(message.Message.(*miniraft.AppendEntriesRequest))
			case miniraft.AppendEntriesResponseMessage:
				state.handleAppendEntriesResponse(message.Message.(*miniraft.AppendEntriesResponse))
			case miniraft.RequestVoteRequestMessage:
				state.handleRequestVoteRequest(message.Message.(*miniraft.RequestVoteRequest))
			case miniraft.RequestVoteResponseMessage:
				state.handleRequestVoteResponse(message.Message.(*miniraft.RequestVoteResponse))
			}
		}
	}
}