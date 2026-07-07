package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/fileset"
	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

func verifyCmd() *cobra.Command {
	var all bool
	var output string

	cmd := &cobra.Command{
		Use:   "verify [model_id]",
		Short: "Re-verify a downloaded model against canonical HuggingFace hashes",
		Long: "Re-hash a downloaded model and run the completeness gate — no re-download.\n" +
			"Proves the on-disk bytes still match upstream, catching partial downloads\n" +
			"and bitrot. Canonical hashes come from the repo's file listing (LFS oid).",
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("--all takes no model argument")
			}
			if !all && len(args) == 0 {
				return fmt.Errorf("provide a model id (org/model) or use --all")
			}

			client := newAPIClient(cmd)
			dirs := config.Dirs()
			reg := registry.New(dirs.Data)
			if err := reg.Load(); err != nil {
				return err
			}

			var targets []string
			if all {
				for _, m := range reg.List() {
					targets = append(targets, m.ID)
				}
				if len(targets) == 0 {
					fmt.Println("\n  No downloaded models to verify.")
					return nil
				}
			} else {
				targets = []string{args[0]}
			}

			failed := 0
			for _, id := range targets {
				if err := verifyOne(cmd.Context(), client, reg, id, output); err != nil {
					failed++
				}
			}
			fmt.Println()
			if failed > 0 {
				return fmt.Errorf("%d of %d model(s) failed verification", failed, len(targets))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Verify every downloaded model (cron-able bitrot sweep)")
	cmd.Flags().StringVar(&output, "output", "", "Verify a model in a specific directory instead of the registry path")
	tokenFlag(cmd)
	return cmd
}

// verifyOne re-verifies one downloaded model against canonical upstream hashes.
// It verifies what the registry records as downloaded, at each file's recorded
// LocalPath: GGUF quant shards are re-hashed directly (the safetensors gate does
// not apply to them), while a safetensors/vLLM fileset — or a model not tracked
// in the registry — goes through the completeness gate. It prints a per-model
// result and returns an error iff the model is not serve-ready, so the caller
// can tally failures across an --all sweep.
func verifyOne(ctx context.Context, client *api.Client, reg *registry.Registry, modelID, output string) error {
	headerStyle := lipgloss.NewStyle().Bold(true)
	fmt.Printf("\n  %s %s\n", headerStyle.Render("Verifying"), modelID)

	repoFiles, err := client.ListFiles(ctx, modelID)
	if err != nil {
		fmt.Printf("  ✗ %v\n", err)
		return err
	}

	// Partition what the registry recorded: GGUF quant shards are re-hashed
	// against upstream directly; everything else is a safetensors/vLLM fileset
	// verified by the completeness gate.
	lm := reg.Get(modelID)
	var ggufRefs []fileset.FileRef
	hasSafetensors := false
	if lm != nil {
		for _, f := range lm.Files {
			if !f.Complete {
				continue
			}
			name := strings.ToLower(f.Filename)
			switch {
			case strings.HasSuffix(name, ".gguf"):
				ggufRefs = append(ggufRefs, fileset.FileRef{
					Filename:  f.Filename,
					LocalPath: ggufLocalPath(f, output, reg.ModelDir(modelID)),
				})
			case strings.HasSuffix(name, ".safetensors"):
				hasSafetensors = true
			}
		}
	}

	failed := false

	// GGUF: re-hash the recorded quant shards against upstream (no gate).
	if len(ggufRefs) > 0 {
		rep := fileset.VerifyFiles(repoFiles, ggufRefs)
		if !printVerifyReport(rep, fmt.Sprintf("%d GGUF file(s) present and hash-matched", len(ggufRefs))) {
			failed = true
		}
	}

	// Safetensors/vLLM fileset, or a model not tracked in the registry (fall
	// back to the directory gate). A GGUF model with a few loose config files
	// must NOT trip the safetensors gate — only an actual safetensors weight set
	// (or an untracked model) does.
	if hasSafetensors || lm == nil {
		localDir := output
		if localDir == "" {
			localDir = reg.ModelDir(modelID)
		}
		if _, statErr := os.Stat(localDir); statErr != nil {
			if len(ggufRefs) == 0 {
				fmt.Printf("  ✗ not downloaded (%s)\n", localDir)
				return fmt.Errorf("%s: not downloaded", modelID)
			}
		} else {
			rep, err := fileset.Verify(repoFiles, localDir)
			if err != nil {
				fmt.Printf("  ✗ %v\n", err)
				return err
			}
			if !printVerifyReport(rep, "all required files present and hash-matched") {
				failed = true
			}
		}
	}

	if failed {
		return fmt.Errorf("%s: verification failed", modelID)
	}
	return nil
}

// ggufLocalPath resolves where a recorded GGUF file lives: its registry
// LocalPath normally, or under the --output override directory when set. When
// neither is available it falls back to the registry's model dir — never to a
// bare (cwd-relative) filename, which could verify an unrelated file in the
// working directory and pass without proving the registered download exists.
func ggufLocalPath(f registry.LocalFile, output, modelDir string) string {
	if output != "" {
		return filepath.Join(output, filepath.Base(f.Filename))
	}
	if f.LocalPath != "" {
		return f.LocalPath
	}
	return filepath.Join(modelDir, filepath.Base(f.Filename))
}

// printVerifyReport renders a completeness report's warnings and failures and
// returns whether it passed (no hard failures).
func printVerifyReport(rep *fileset.Report, okMsg string) bool {
	for _, w := range rep.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
	if !rep.Complete() {
		for _, f := range rep.HardFail {
			fmt.Printf("  ✗ %s\n", f)
		}
		return false
	}
	fmt.Printf("  ✓ %s\n", okMsg)
	return true
}
