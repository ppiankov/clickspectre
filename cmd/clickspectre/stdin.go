package main

import (
	"bufio"
	"os"
)

// readStdinLines reads newline-delimited input from stdin.
func readStdinLines() []string {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
