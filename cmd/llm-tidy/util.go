package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/internal/progress"
	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

// formatSize delegates to the shared formatter used elsewhere in spark-tools.
func formatSize(bytes int64) string { return progress.FormatSize(bytes) }

// tolerateInventory turns a partial-inventory error (a backend was unreachable,
// e.g. no Ollama daemon on a GGUF-only box) into a warning and proceeds, per
// spec §5.3 skip-with-warning. Any other error is fatal and returned unchanged.
// The DiffResult from Diff remains valid over the backends that did respond.
func tolerateInventory(w io.Writer, err error) error {
	var partial *llmtidy.PartialInventoryError
	if errors.As(err, &partial) {
		fmt.Fprintf(w, "%s %s\n", styleHint.Render("⚠ backend unavailable:"), partial.Error())
		return nil
	}
	return err
}

// parseDuration extends time.ParseDuration with "d" for days, supporting
// the "7d" / "30d" forms shown in spec §7.1. An empty string means "no cutoff".
// A non-empty value that resolves to a non-positive duration (e.g. "-7d", "0")
// is rejected: --older-than gates deletion, and a non-positive cutoff would be
// silently treated as "no age filter" downstream, pruning every untracked model.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var d time.Duration
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		d = time.Duration(days) * 24 * time.Hour
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, err
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

// resolveBackend parses a --backend flag value or returns BackendUnknown
// when the flag was not set.
func resolveBackend(flag string) (inventory.ModelBackend, error) {
	if flag == "" {
		return inventory.BackendUnknown, nil
	}
	return inventory.ParseBackend(flag)
}

// modelsBy filters installed models by backend.
func modelsBy(models []llmtidy.InstalledModel, b inventory.ModelBackend) []llmtidy.InstalledModel {
	var out []llmtidy.InstalledModel
	for _, m := range models {
		if m.Backend == b {
			out = append(out, m)
		}
	}
	return out
}

// humanAge formats a time as "N days ago" / "today" for the status table.
func humanAge(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	diff := now.Sub(t)
	days := int(diff.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}
