// Package paths provides shared XDG base-directory and home-resolution
// MECHANISM — not policy. Each tool keeps ownership of its own app name, env-var
// prefix, directory layout, and error handling; these helpers only remove the
// duplicated XDG-resolution arithmetic that was copied across hfetch, llm-run,
// and llm-serve.
//
// Deliberately NOT here: a single global Spark Tools directory policy. Tools
// with divergent shapes keep their own resolvers — notably llm-tidy, which
// propagates a home-resolution error rather than degrading best-effort (see
// docs/internal-extraction-map.md).
package paths

import (
	"os"
	"path/filepath"
)

// Home returns the user's home directory best-effort: on failure it returns ""
// rather than erroring. A "" home yields relative fallback paths (e.g.
// ".config/<app>") via filepath.Join — the long-standing behavior of hfetch,
// llm-run, and llm-serve. Callers that must FAIL on a missing home (llm-tidy)
// call os.UserHomeDir directly; do not change this to error.
func Home() string {
	home, _ := os.UserHomeDir()
	return home
}

// xdg resolves an XDG base directory: $<envKey>/<app> when the env var is set,
// otherwise Home() joined with the fallback segments and <app>.
func xdg(envKey, app string, fallback ...string) string {
	if v := os.Getenv(envKey); v != "" {
		return filepath.Join(v, app)
	}
	segs := make([]string, 0, len(fallback)+2)
	segs = append(segs, Home())
	segs = append(segs, fallback...)
	segs = append(segs, app)
	return filepath.Join(segs...)
}

// XDGConfig resolves $XDG_CONFIG_HOME/<app> or ~/.config/<app>.
func XDGConfig(app string) string { return xdg("XDG_CONFIG_HOME", app, ".config") }

// XDGData resolves $XDG_DATA_HOME/<app> or ~/.local/share/<app>.
func XDGData(app string) string { return xdg("XDG_DATA_HOME", app, ".local", "share") }

// XDGCache resolves $XDG_CACHE_HOME/<app> or ~/.cache/<app>.
func XDGCache(app string) string { return xdg("XDG_CACHE_HOME", app, ".cache") }

// XDGState resolves $XDG_STATE_HOME/<app> or ~/.local/state/<app>.
func XDGState(app string) string { return xdg("XDG_STATE_HOME", app, ".local", "state") }
