package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/reconcile"
)

func syncCmd() *cobra.Command {
	var (
		backend string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull models in manifest that are missing locally",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := resolveBackend(backend)
			if err != nil {
				return err
			}
			tidy, err := newTidy(cmd)
			if err != nil {
				return err
			}
			return runSync(cmd.Context(), cmd.OutOrStdout(), tidy, b, dryRun)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "filter to one backend (ollama|gguf)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan without executing")
	return cmd
}

func runSync(ctx context.Context, w io.Writer, tidy *llmtidy.Tidy, b inventory.ModelBackend, dryRun bool) error {
	diff, err := tidy.Diff(ctx)
	if err != nil {
		if errors.Is(err, llmtidy.ErrManifestNotFound) {
			return fmt.Errorf("no manifest found at %s\nRun: llm-tidy init", tidy.ManifestPath())
		}
		return err
	}

	var backendPtr *inventory.ModelBackend
	if b != inventory.BackendUnknown {
		x := b
		backendPtr = &x
	}
	plan := reconcile.SyncPlan(*diff, reconcile.SyncOptions{Backend: backendPtr})
	if len(plan) == 0 {
		fmt.Fprintln(w, "Already in sync.")
		return nil
	}

	fmt.Fprintln(w, "The following models will be pulled:")
	for _, s := range plan {
		fmt.Fprintf(w, "  ● [%s] %s\n", s.Backend.String(), s.Name())
	}
	fmt.Fprintln(w)

	if dryRun {
		fmt.Fprintln(w, styleHint.Render("(dry-run; nothing fetched)"))
		return nil
	}

	pulled, err := tidy.Sync(ctx)
	for _, m := range pulled {
		fmt.Fprintf(w, "  ✓ %s\n", m.Name)
	}
	if err != nil {
		fmt.Fprintf(w, "\nSync finished with errors: %v\n", err)
		return err
	}
	fmt.Fprintf(w, "\nPulled %d models.\n", len(pulled))
	return nil
}
