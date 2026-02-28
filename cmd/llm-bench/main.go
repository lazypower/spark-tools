package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-bench: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("llm-bench - LLM benchmark suite")
	return nil
}
