package main

import "net"

type ClientCommand struct {
	Command string
}

type Client struct {
	Addr    *net.UDPAddr
	AddrStr string
	conn    *net.UDPConn
}
