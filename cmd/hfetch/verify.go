package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/fileset"
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
				localDir := output
				if localDir == "" {
					localDir = reg.ModelDir(id)
				}
				if err := verifyOne(cmd.Context(), client, id, localDir); err != nil {
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

// verifyOne runs the completeness gate for one model against canonical upstream
// hashes. It prints a per-model result and returns an error iff the model is
// not serve-ready, so the caller can tally failures across an --all sweep.
func verifyOne(ctx context.Context, client *api.Client, modelID, localDir string) error {
	headerStyle := lipgloss.NewStyle().Bold(true)
	fmt.Printf("\n  %s %s\n", headerStyle.Render("Verifying"), modelID)

	if _, err := os.Stat(localDir); err != nil {
		fmt.Printf("  ✗ not downloaded (%s)\n", localDir)
		return fmt.Errorf("%s: not downloaded", modelID)
	}

	repoFiles, err := client.ListFiles(ctx, modelID)
	if err != nil {
		fmt.Printf("  ✗ %v\n", err)
		return err
	}

	rep, err := fileset.Verify(repoFiles, localDir)
	if err != nil {
		fmt.Printf("  ✗ %v\n", err)
		return err
	}
	for _, w := range rep.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
	if !rep.Complete() {
		for _, f := range rep.HardFail {
			fmt.Printf("  ✗ %s\n", f)
		}
		return fmt.Errorf("%s: incomplete (%d issue(s))", modelID, len(rep.HardFail))
	}
	fmt.Printf("  ✓ OK — all required files present and hash-matched\n")
	return nil
}
