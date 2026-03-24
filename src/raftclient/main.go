package main

import "log"

func main() {
	c := Client{}

	addr, err := parseFlags()
	if err != nil {
		log.Fatalf("Error in parseFlags():\n%q", err)
	}
	c.AddrStr = addr

	if err = c.initConn(); err != nil {
		log.Fatalf("Error in initConn():\n%v", err)
	}
	defer c.conn.Close()

	if err := c.parseInput(); err != nil {
		log.Fatalf("Error in parseInput():\n%v", err)
	}
}
