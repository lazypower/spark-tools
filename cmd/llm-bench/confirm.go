package main

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/term"
)

// confirmRun prompts for confirmation. Returns true if user confirms.
// Skips the prompt if stdin is not a terminal.
func confirmRun() bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true // Non-interactive: don't block
	}

	fmt.Print("Press Enter to start (or Ctrl-C to abort)... ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return true
}
