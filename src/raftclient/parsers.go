package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
)

func (c *Client) parseInput() error {
	scanner := bufio.NewScanner(os.Stdin)
	var regexCmd = regexp.MustCompile(`^[a-z0-9_-]+$`)

	for scanner.Scan() {
		line := strings.TrimSpace(strings.ToLower(scanner.Text()))

		if line == "exit" {
			return nil
		}
		if !regexCmd.MatchString(line) {
			log.Printf("Invalid command: %q", line)
			continue
		}

		if err := c.sendCommand(line); err != nil {
			log.Printf("sendCommand failed: %v", err)
		}
	}
	return scanner.Err()
}

func parseFlags() (string, error) {
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		return "", fmt.Errorf("Usage: go run ./client network:port")
	}
	return args[0], nil
}
