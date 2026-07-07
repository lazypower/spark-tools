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
	if errors.Is(err, llmtidy.ErrManifestNotFound) {
		return fmt.Errorf("no manifest found at %s\nRun: llm-tidy init", tidy.ManifestPath())
	}
	// An unreachable backend is a warning, not a failure: sync the backends
	// that did respond rather than being unusable on a GGUF-only box.
	partial, err := tolerateInventory(w, err)
	if err != nil {
		return err
	}

	var backendPtr *inventory.ModelBackend
	if b != inventory.BackendUnknown {
		x := b
		backendPtr = &x
	}
	plan := reconcile.SyncPlan(*diff, reconcile.SyncOptions{Backend: backendPtr})
	if partial {
		// Drop specs whose backend is unreachable: pulling from a down backend
		// would fail the spec and turn a tolerated sync into a hard error.
		plan = skipUnavailableSpecs(w, plan, tidy.Provider().Probe(ctx))
	}
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

	pulled, err := tidy.Sync(ctx, plan)
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

// skipUnavailableSpecs drops sync specs whose backend is not currently
// reachable, printing a per-spec skip note. It keeps a tolerated sync from
// attempting — and failing — a pull against a backend that is down.
func skipUnavailableSpecs(w io.Writer, plan []reconcile.ModelSpec, avail inventory.Available) []reconcile.ModelSpec {
	backendUp := func(b inventory.ModelBackend) bool {
		switch b {
		case inventory.BackendOllama:
			return avail.Ollama
		case inventory.BackendGGUF:
			return avail.GGUF
		case inventory.BackendVLLM:
			return avail.VLLM
		default:
			return true
		}
	}
	var kept []reconcile.ModelSpec
	for _, s := range plan {
		if !backendUp(s.Backend) {
			fmt.Fprintf(w, "  %s [%s] %s (backend unavailable)\n", styleHint.Render("⊘ skipped"), s.Backend.String(), s.Name())
			continue
		}
		kept = append(kept, s)
	}
	return kept
}
