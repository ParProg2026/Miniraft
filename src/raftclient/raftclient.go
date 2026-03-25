package main

import (
	"fmt"
	"os"
)

// RaftClient is responsible for parsing user input from standard input
// and sending it to a specified Raft server over UDP.
//
// Usage:
//   go run raftclient.go server-host:server-port
//
// Behavior:
//   - Connects via UDP to the given server address.
//   - Scans standard input for string commands (e.g., lowercase/uppercase letters and digits, no spaces).
//   - If "exit" is inputted, the client terminates.
//   - For any other valid command, sends it to the server.
//   - Important: The client DOES NOT block or wait for any acknowledgement from the server
//     that the command was committed.

func _main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <server-host:server-port>\n", os.Args[0])
		os.Exit(1)
	}

	serverAddr := os.Args[1]
	fmt.Printf("Starting Raft client connecting to %s...\n", serverAddr)

	// TODO: Set up UDP socket / connection to the `serverAddr`.

	// TODO: Create a loop scanning `os.Stdin` for user input.
	//   - Handle "exit" to terminate.
	//   - Validate input strictly to `[a-zA-Z0-9_-]` if needed.
	//   - Send the command to the server via UDP.
}
