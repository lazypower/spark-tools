package interlock

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// LLMServeChecker is a Checker backed by `llm-serve liveness --check`: it pipes
// the candidate paths to the binary's stdin and reads the protected subset from
// stdout (the overlap is computed by llm-serve — the one authority), and the
// complaint warnings from stderr. bin defaults to "llm-serve" on PATH.
func LLMServeChecker(bin string) Checker {
	if bin == "" {
		bin = "llm-serve"
	}
	return func(ctx context.Context, paths []string) (protected, warnings []string, err error) {
		if _, err := exec.LookPath(bin); err != nil {
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
