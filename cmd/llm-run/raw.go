package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	hfconfig "github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/config"
	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
	"github.com/lazypower/spark-tools/pkg/llmrun/resolver"
)

func rawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "raw <model> -- <args>",
		Short: "Pass raw args directly to llama.cpp",
		Long: `Run llama.cpp with raw arguments. This is the escape hatch for
anything llm-run doesn't wrap. The model is resolved via hfetch,
then all arguments after -- are passed directly to llama-server or llama-cli.

Example:
  llm-run raw qwen-32b -- --ctx-size 4096 --temp 0.5 --verbose`,
		Args:                  cobra.MinimumNArgs(1),
		DisableFlagParsing:    false,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Split args at -- separator.
			modelRef := args[0]
			var rawArgs []string
			if cmd.ArgsLenAtDash() > 0 {
				rawArgs = args[cmd.ArgsLenAtDash():]
			}

			return runRaw(modelRef, rawArgs)
		},
	}

	return cmd
}

func runRaw(modelRef string, rawArgs []string) error {
	dirs := config.Dirs()
	gcfg := config.LoadGlobalConfig()

	// Resolve model path.
	hfDirs := hfconfig.Dirs()
	res := resolver.NewResolver(dirs.Config, hfDirs.Data)
	resolved, err := res.ResolveModel(context.Background(), modelRef)
	if err != nil {
		return err
	}
	if resolved.Path == "" {
		return fmt.Errorf("model not found locally: %s\n  Use 'hfetch pull %s' to download it first", modelRef, modelRef)
	}

	// Detect llama.cpp.
	caps, err := engine.DetectBinaries(gcfg.LlamaDir)
	if err != nil {
		return fmt.Errorf("llama.cpp not found: %w", err)
	}

	// Determine which binary to use. Default to llama-cli for raw mode.
	binary := lookupRawBinary(caps)

	// Build command: binary --model <path> <raw args...>
	cmdArgs := []string{"--model", resolved.Path}
	cmdArgs = append(cmdArgs, rawArgs...)

	fmt.Printf("  Running: %s --model %s", binary, resolved.Path)
	for _, a := range rawArgs {
		fmt.Printf(" %s", a)
	}
	fmt.Println()
	fmt.Println()

	// Execute the binary, forwarding stdio.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	proc := exec.CommandContext(ctx, binary, cmdArgs...)
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr

	return proc.Run()
}

func lookupRawBinary(caps *engine.Capabilities) string {
	// Prefer llama-cli for raw mode; fall back to llama-server.
	if caps.BinaryDir != "" {
		cli := caps.BinaryDir + "/llama-cli"
		if _, err := os.Stat(cli); err == nil {
			return cli
		}
	}
	// Check PATH.
	if path, err := exec.LookPath("llama-cli"); err == nil {
		return path
	}
	// Fall back to whatever binary we detected.
	return caps.BinaryPath
}
