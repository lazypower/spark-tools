package interlock

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LLMServeChecker is a Checker backed by `llm-serve liveness --check`: it pipes
// the candidate paths to the binary's stdin and reads the protected subset from
// stdout (the overlap is computed by llm-serve — the one authority), and the
// complaint warnings from stderr.
//
// Resolving the binary: an explicit bin arg wins; else $LLM_SERVE_BIN; else
// "llm-serve" on PATH. The env override matters because llm-serve is often
// installed somewhere not on a non-interactive/cron PATH (e.g. ~/.local/bin) —
// without it, the interlock would mistake "not on PATH" for "not installed" and
// go inactive (fail-open).
//
// Explicit-config is a fail-CLOSED signal. When the operator names the binary
// (bin arg or $LLM_SERVE_BIN), they are declaring "llm-serve manages this box";
// if that named binary then can't be resolved it's a MISCONFIGURATION, not
// absence — so we return a hard error (caller fails closed, blocks every
// path-based candidate) rather than ErrLLMServeAbsent. Only the unset default
// ("llm-serve" not on PATH) means genuinely-not-deployed → inactive, which is the
// right pass-through for ollama/gguf-only boxes that never ran llm-serve.
func LLMServeChecker(bin string) Checker {
	explicit := bin != ""
	if bin == "" {
		if env := os.Getenv("LLM_SERVE_BIN"); env != "" {
			bin, explicit = env, true
		}
	}
	if bin == "" {
		bin = "llm-serve"
	}
	return func(ctx context.Context, paths []string) (protected, warnings []string, err error) {
		if _, err := exec.LookPath(bin); err != nil {
			if explicit {
				return nil, nil, fmt.Errorf("llm-serve binary %q is configured but unresolvable (interlock fails closed; fix the path/PATH, or unset LLM_SERVE_BIN to disable the interlock on a box that doesn't run llm-serve): %w", bin, err)
			}
			return nil, nil, ErrLLMServeAbsent
		}
		cmd := exec.CommandContext(ctx, bin, "liveness", "--check")
		cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
		var out, errb bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			return nil, nil, fmt.Errorf("llm-serve liveness --check: %w: %s", err, strings.TrimSpace(errb.String()))
		}
		return splitNonEmpty(out.String()), splitNonEmpty(errb.String()), nil
	}
}

// splitNonEmpty splits on newlines and keeps non-empty lines WITHOUT trimming
// their content — a path may legitimately have leading/trailing spaces, and
// trimming would desync the echo-match against the candidate's Path. A trailing
// \r (if any) is stripped, but spaces are preserved.
func splitNonEmpty(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
