package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/reconcile"
)

var (
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleBlessed = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleUntrack = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleHint    = lipgloss.NewStyle().Faint(true)
)

func statusCmd() *cobra.Command {
	var (
		backend string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show inventory vs. manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := resolveBackend(backend)
			if err != nil {
				return err
			}
			tidy, err := newTidy(cmd)
			if err != nil {
				return err
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), tidy, b, asJSON)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "filter to one backend (ollama|gguf)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func runStatus(ctx context.Context, w io.Writer, tidy *llmtidy.Tidy, b inventory.ModelBackend, asJSON bool) error {
	avail := tidy.Provider().Probe(ctx)

	m, manErr := tidy.LoadManifest()
	if manErr != nil && !errors.Is(manErr, llmtidy.ErrManifestNotFound) {
		return manErr
	}
	if errors.Is(manErr, llmtidy.ErrManifestNotFound) {
		fmt.Fprintln(w, styleHint.Render("No manifest found at "+tidy.ManifestPath()))
		fmt.Fprintln(w, styleHint.Render("Run: llm-tidy init"))
		fmt.Fprintln(w)
		m = &llmtidy.Manifest{}
	}

	inv, invErr := tidy.Inventory(ctx)
	// invErr is non-nil if a configured backend failed; we still want
	// partial output for the side that did respond.
	d := reconcile.Diff(m, inv)

	if asJSON {
		return emitStatusJSON(w, d, avail, invErr)
	}

	now := time.Now()
	if b == inventory.BackendUnknown || b == inventory.BackendOllama {
		renderBackend(w, "Ollama Models", inventory.BackendOllama, d, avail.Ollama, now)
		fmt.Fprintln(w)
	}
	if b == inventory.BackendUnknown || b == inventory.BackendGGUF {
		renderBackend(w, "GGUF Models", inventory.BackendGGUF, d, avail.GGUF, now)
		fmt.Fprintln(w)
	}

	if hasMissing(d) {
		fmt.Fprintln(w, styleHeader.Render("MISSING"))
		for _, s := range d.Missing {
			if b != inventory.BackendUnknown && s.Backend != b {
				continue
			}
			fmt.Fprintln(w, "  ●", s.Name())
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, styleHint.Render("Use 'llm-tidy prune' to remove untracked models."))
	fmt.Fprintln(w, styleHint.Render("Use 'llm-tidy promote <model>' to add a model to your manifest."))

	if invErr != nil {
		fmt.Fprintln(os.Stderr, styleHint.Render("Note: "+invErr.Error()))
	}
	return nil
}

func renderBackend(w io.Writer, title string, b inventory.ModelBackend, d reconcile.DiffResult, available bool, now time.Time) {
	blessed := modelsBy(d.Blessed, b)
	untracked := modelsBy(d.Untracked, b)
	fmt.Fprintln(w, styleHeader.Render(fmt.Sprintf("%s (%d blessed, %d untracked)", title, len(blessed), len(untracked))))

	if !available && len(blessed) == 0 && len(untracked) == 0 {
		fmt.Fprintln(w, styleHint.Render("  backend not available"))
		return
	}

	if len(blessed) > 0 {
		fmt.Fprintln(w, "  BLESSED")
		for _, m := range blessed {
			fmt.Fprintf(w, "  %s %-42s %10s\n",
				styleBlessed.Render("✓"),
				m.Name,
				formatSize(m.Size))
		}
	}
	if len(untracked) > 0 {
		fmt.Fprintln(w, "  UNTRACKED")
		for _, m := range untracked {
			fmt.Fprintf(w, "  %s %-42s %10s   %s\n",
				styleUntrack.Render("●"),
				m.Name,
				formatSize(m.Size),
				humanAge(m.Modified, now))
		}
		fmt.Fprintf(w, "\n  Untracked total: %s across %d models\n",
			formatSize(reconcile.TotalSize(untracked)),
			len(untracked))
	}
}

func hasMissing(d reconcile.DiffResult) bool { return len(d.Missing) > 0 }

type statusJSON struct {
	Ollama    sideJSON      `json:"ollama"`
	GGUF      sideJSON      `json:"gguf"`
	Missing   []missingJSON `json:"missing"`
	Available availJSON     `json:"available"`
	Note      string        `json:"note,omitempty"`
}

type sideJSON struct {
	Blessed   []modelJSON `json:"blessed"`
	Untracked []modelJSON `json:"untracked"`
}

type modelJSON struct {
	Name     string    `json:"name"`
	Repo     string    `json:"repo,omitempty"`
	Quant    string    `json:"quant,omitempty"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified,omitempty"`
}

type missingJSON struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Repo    string `json:"repo,omitempty"`
	Quant   string `json:"quant,omitempty"`
}

type availJSON struct {
	Ollama bool `json:"ollama"`
	GGUF   bool `json:"gguf"`
}

func emitStatusJSON(w io.Writer, d reconcile.DiffResult, avail inventory.Available, invErr error) error {
	out := statusJSON{
		Available: availJSON{Ollama: avail.Ollama, GGUF: avail.GGUF},
	}
	if invErr != nil {
		out.Note = invErr.Error()
	}

	for _, m := range d.Blessed {
		j := modelJSON{Name: m.Name, Repo: m.Repo, Quant: m.Quant, Size: m.Size, Modified: m.Modified}
		switch m.Backend {
		case inventory.BackendOllama:
			out.Ollama.Blessed = append(out.Ollama.Blessed, j)
		case inventory.BackendGGUF:
			out.GGUF.Blessed = append(out.GGUF.Blessed, j)
		}
	}
	for _, m := range d.Untracked {
		j := modelJSON{Name: m.Name, Repo: m.Repo, Quant: m.Quant, Size: m.Size, Modified: m.Modified}
		switch m.Backend {
		case inventory.BackendOllama:
			out.Ollama.Untracked = append(out.Ollama.Untracked, j)
		case inventory.BackendGGUF:
			out.GGUF.Untracked = append(out.GGUF.Untracked, j)
		}
	}
	for _, s := range d.Missing {
		mj := missingJSON{Backend: s.Backend.String(), Name: s.Name()}
		if s.GGUF != nil {
			mj.Repo = s.GGUF.Repo
			mj.Quant = s.GGUF.Quant
		}
		out.Missing = append(out.Missing, mj)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
