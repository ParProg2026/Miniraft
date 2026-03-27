package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	miniraft "raft/protocol"
)

type PacketType int

const (
	IncomingUnknown PacketType = iota
	IncomingClientCommand
	IncomingAppendEntriesRequest
	IncomingAppendEntriesResponse
	IncomingRequestVoteRequest
	IncomingRequestVoteResponse
)

type IncomingPacket struct {
	From    net.UDPAddr
	Type    PacketType
	Payload any
	Raw     []byte
}

func decodeIncomingPacket(from net.UDPAddr, data []byte) (*IncomingPacket, error) {
	// First inspect the top-level keys.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Client command first.
	// The assignment says client packets look like {"Command":"..."}.
	if raw, ok := probe["Command"]; ok && len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var cmd ClientCommand
		if err := decodeStrict(data, &cmd); err != nil {
			return nil, fmt.Errorf("invalid ClientCommand: %w", err)
		}
		if cmd.Command == "" {
			return nil, fmt.Errorf("empty client command")
		}
		return &IncomingPacket{
			From:    from,
			Type:    IncomingClientCommand,
			Payload: &cmd,
			Raw:     append([]byte(nil), data...),
		}, nil
	}

	// Raft requests next.
	if raw, ok := probe["LeaderId"]; ok && len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var req miniraft.AppendEntriesRequest
		if err := decodeStrict(data, &req); err != nil {
			return nil, fmt.Errorf("invalid AppendEntriesRequest: %w", err)
		}
		if req.LeaderId == "" {
			return nil, fmt.Errorf("AppendEntriesRequest missing LeaderId")
		}
		return &IncomingPacket{
			From:    from,
			Type:    IncomingAppendEntriesRequest,
			Payload: &req,
			Raw:     append([]byte(nil), data...),
		}, nil
	}

	if raw, ok := probe["CandidateName"]; ok && len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var req miniraft.RequestVoteRequest
		if err := decodeStrict(data, &req); err != nil {
			return nil, fmt.Errorf("invalid RequestVoteRequest: %w", err)
		}
		if req.CandidateName == "" {
			return nil, fmt.Errorf("RequestVoteRequest missing CandidateName")
		}
		return &IncomingPacket{
			From:    from,
			Type:    IncomingRequestVoteRequest,
			Payload: &req,
			Raw:     append([]byte(nil), data...),
		}, nil
	}

	// Raft responses last.
	if _, ok := probe["VoteGranted"]; ok {
		var resp miniraft.RequestVoteResponse
		if err := decodeStrict(data, &resp); err != nil {
			return nil, fmt.Errorf("invalid RequestVoteResponse: %w", err)
		}
		return &IncomingPacket{
			From:    from,
			Type:    IncomingRequestVoteResponse,
			Payload: &resp,
			Raw:     append([]byte(nil), data...),
		}, nil
	}

	if _, ok := probe["Success"]; ok {
		var resp miniraft.AppendEntriesResponse
		if err := decodeStrict(data, &resp); err != nil {
			return nil, fmt.Errorf("invalid AppendEntriesResponse: %w", err)
		}
		return &IncomingPacket{
			From:    from,
			Type:    IncomingAppendEntriesResponse,
			Payload: &resp,
			Raw:     append([]byte(nil), data...),
		}, nil
	}

	return nil, fmt.Errorf("unknown message shape: %s", string(data))
}

func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
