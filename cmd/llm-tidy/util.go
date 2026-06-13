package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/internal/progress"
	"github.com/lazypower/spark-tools/pkg/llmtidy"
	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

// formatSize delegates to the shared formatter used elsewhere in spark-tools.
func formatSize(bytes int64) string { return progress.FormatSize(bytes) }

// parseDuration extends time.ParseDuration with "d" for days, supporting
// the "7d" / "30d" forms shown in spec §7.1.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
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
