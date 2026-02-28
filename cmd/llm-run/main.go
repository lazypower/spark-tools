package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-run: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("llm-run - llama.cpp wrapper for humans")
	return nil
}
