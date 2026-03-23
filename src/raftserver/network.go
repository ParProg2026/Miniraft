package main

import (
	"fmt"
	"log"
	"net"
	miniraft "raft/protocol"
)

// MaxPacketSize defines the maximum allowed UDP packet size as per the academic requirements.
const MaxPacketSize = 1400

// NetworkManager handles all UDP communication for the Raft server.
// It encapsulates the connection state and peer list to adhere to the Single Responsibility Principle.
type NetworkManager struct {
	Conn  *net.UDPConn
	Peers []string
}

// InitListener binds the UDP socket to the specified identity address.
// It returns an error if the address cannot be resolved or bound.
func (nm *NetworkManager) InitListener(identity string) error {
	addr, err := net.ResolveUDPAddr("udp", identity)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address %s: %w", identity, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP %s: %w", identity, err)
	}

	nm.Conn = conn
	return nil
}

// SendRaftMessage serializes and sends a dynamic Raft message to a specific peer.
// It enforces the maximum packet size constraint before transmitting over the network.
func (nm *NetworkManager) SendRaftMessage(peerAddr string, payload any) error {
	msg := &miniraft.RaftMessage{Message: payload}

	data, err := msg.MarshalJson()
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for %s: %w", peerAddr, err)
	}

	if len(data) > MaxPacketSize {
		return fmt.Errorf("packet exceeds %d byte limit: %d bytes", MaxPacketSize, len(data))
	}

	addr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve peer address %s: %w", peerAddr, err)
	}

	_, err = nm.Conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("failed to write to UDP %s: %w", peerAddr, err)
	}

	return nil
}

// BroadcastRaftMessage sends the provided payload to all known peers in the cluster.
// It silently logs individual transmission failures without halting the broadcast loop.
func (nm *NetworkManager) BroadcastRaftMessage(payload any) {
	for _, peer := range nm.Peers {
		if err := nm.SendRaftMessage(peer, payload); err != nil {
			log.Printf("Broadcast failed for peer %s: %v\n", peer, err)
		}
	}
}

// ListenLoop continuously reads from the UDP connection and dispatches incoming messages.
// It accepts a callback handler function to process the parsed messages, completely
// decoupling the networking layer from the consensus state logic.
func (nm *NetworkManager) ListenLoop(messageHandler func(net.Addr, miniraft.MessageType, any)) {
	buffer := make([]byte, MaxPacketSize)

	for {
		n, addr, err := nm.Conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Error reading from UDP: %v\n", err)
			continue
		}

		var message miniraft.RaftMessage
		msgType, err := message.UnmarshalJSON(buffer[:n])
		if err != nil {
			log.Printf("Error unmarshalling incoming message from %s: %v\n", addr.String(), err)
			continue
		}

		// Dispatch the successfully parsed message to the injected handler.
		messageHandler(addr, msgType, message.Message)
	}
}
