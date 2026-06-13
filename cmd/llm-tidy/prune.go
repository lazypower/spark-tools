package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/internal/ui"
	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/reconcile"
)

func pruneCmd() *cobra.Command {
	var (
		backend   string
		dryRun    bool
		yes       bool
		olderThan string
	)
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove models not in manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := resolveBackend(backend)
			if err != nil {
				return err
			}
			d, err := parseDuration(olderThan)
			if err != nil {
				return err
			}
			tidy, err := newTidy(cmd)
			if err != nil {
				return err
			}
			return runPrune(cmd.Context(), cmd.OutOrStdout(), tidy, pruneOpts{
				backend:   b,
				dryRun:    dryRun,
				skipPrompt: yes,
				olderThan: d,
			})
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "filter to one backend (ollama|gguf)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan without executing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "only prune untracked models older than this (e.g. 7d, 30d, 2h)")
	return cmd
}

type pruneOpts struct {
	backend    inventory.ModelBackend
	dryRun     bool
	skipPrompt bool
	olderThan  time.Duration
}

func runPrune(ctx context.Context, w io.Writer, tidy *llmtidy.Tidy, opts pruneOpts) error {
	diff, err := tidy.Diff(ctx)
	if err != nil {
		if errors.Is(err, llmtidy.ErrManifestNotFound) {
			return fmt.Errorf("no manifest found at %s\nRun: llm-tidy init", tidy.ManifestPath())
		}
		return err
	}
	plan := pruneBuildPlan(*diff, opts)
	if len(plan) == 0 {
		fmt.Fprintln(w, "Nothing to prune.")
		return nil
	}

	renderPrunePlan(w, plan)

	if opts.dryRun {
		fmt.Fprintln(w, styleHint.Render("(dry-run; no models removed)"))
		return nil
	}
	if !opts.skipPrompt {
		ok, err := ui.Confirm("Remove these models?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	removed, bytes, err := reconcile.Prune(ctx, tidy.Provider(), plan, func(e reconcile.PruneEvent) {
		if e.Err != nil {
			fmt.Fprintf(w, "  ✗ %s: %v\n", e.Model.Name, e.Err)
			return
		}
		fmt.Fprintf(w, "  ✓ %s\n", e.Model.Name)
	})
	fmt.Fprintf(w, "\nRemoved %d models, reclaimed %s\n", len(removed), formatSize(bytes))
	return err
}

func pruneBuildPlan(d reconcile.DiffResult, opts pruneOpts) []llmtidy.InstalledModel {
	var b *inventory.ModelBackend
	if opts.backend != inventory.BackendUnknown {
		x := opts.backend
		b = &x
	}
	return reconcile.PrunePlan(d, reconcile.PruneOptions{
		Backend:   b,
		OlderThan: opts.olderThan,
	}, time.Now())
}

func renderPrunePlan(w io.Writer, plan []llmtidy.InstalledModel) {
	fmt.Fprintln(w, "The following untracked models will be removed:")
	fmt.Fprintln(w)

	ollama := modelsBy(plan, inventory.BackendOllama)
	gguf := modelsBy(plan, inventory.BackendGGUF)
	if len(ollama) > 0 {
		fmt.Fprintln(w, "Ollama:")
		for _, m := range ollama {
			fmt.Fprintf(w, "  %-42s %10s\n", m.Name, formatSize(m.Size))
		}
	}
	if len(gguf) > 0 {
		fmt.Fprintln(w, "GGUF:")
		for _, m := range gguf {
			fmt.Fprintf(w, "  %-42s %10s\n", m.Name, formatSize(m.Size))
		}
	}
	fmt.Fprintf(w, "\nTotal to reclaim: %s\n\n", formatSize(reconcile.TotalSize(plan)))
}
