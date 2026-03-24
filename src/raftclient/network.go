package main

import (
	"encoding/json"
	"fmt"
	"net"
)

func (c *Client) sendCommand(cmd string) error {
	msg := ClientCommand{Command: cmd}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("Failed to Marshal command: %w", err)
	}

	_, err = c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("Failed to send command: %w", err)
	}

	return nil
}

func (c *Client) initConn() error {
	addr, err := net.ResolveUDPAddr("udp", c.AddrStr)
	if err != nil {
		return fmt.Errorf("Failed to resolve udp address:\n%w", err)
	}
	c.Addr = addr

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("Failed to dial UDP address:\n%w", err)
	}
	c.conn = conn

	return nil
}
