// Package main implements the Raft client, providing a lightweight CLI
// to send state machine commands to a Raft cluster node over UDP.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

// ClientCommand represents the strictly typed JSON payload expected by the Raft server.
// It matches the structure required by the server's strict JSON decoder.
type ClientCommand struct {
	Command string `json:"Command"`
}

// main is the primary entry point for the Raft client.
// It initializes the UDP connection and processes standard input in a continuous loop.
func main() {
	// 1. Validate command line arguments
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <server-host:server-port>\n", os.Args[0])
		os.Exit(1)
	}

	serverAddr := os.Args[1]
	fmt.Printf("Starting Raft client connecting to %s...\n", serverAddr)

	// 2. Resolve the UDP destination address
	udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to resolve server address: %v", err)
	}

	// 3. Establish a connectionless UDP socket
	// DialUDP connects to the remote address. In UDP, this doesn't create a handshake,
	// but it binds the default destination address to the connection object.
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Fatalf("Failed to dial UDP server: %v", err)
	}
	defer conn.Close()

	fmt.Println("Client connected. Enter commands (type 'exit' to quit):")

	// 4. Initialize the standard input scanner
	scanner := bufio.NewScanner(os.Stdin)

	// 5. The Input Processing Loop (Single Responsibility: Read, Serialize, Transmit)
	for scanner.Scan() {
		rawInput := scanner.Text()
		trimmedInput := strings.TrimSpace(rawInput)

		// Handle explicit termination condition
		if strings.ToLower(trimmedInput) == "exit" {
			fmt.Println("Exiting client.")
			break
		}

		// Skip empty inputs to avoid sending useless network traffic
		if trimmedInput == "" {
			continue
		}

		// Construct the strictly typed payload
		cmd := ClientCommand{
			Command: trimmedInput,
		}

		// Serialize the command to JSON for network transmission
		payloadBytes, err := json.Marshal(cmd)
		if err != nil {
			log.Printf("Failed to marshal command: %v\n", err)
			continue
		}

		// Transmit the payload over the UDP socket
		// We use conn.Write() because DialUDP already established the destination.
		_, err = conn.Write(payloadBytes)
		if err != nil {
			log.Printf("Failed to send command to server: %v\n", err)
		} else {
			fmt.Printf("-> Transmitted: %s\n", string(payloadBytes))
		}
	}

	// Catch any fatal errors that occurred during the scanning process
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading standard input: %v", err)
	}
}
