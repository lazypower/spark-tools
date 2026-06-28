package seam

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmtidy/interlock"
)

// Seam: llm-tidy's eviction interlock (the shell-out client) <-> the
// `llm-serve liveness --check` stdin/stdout wire protocol it depends on.
//
// CONTRACT: llm-tidy writes candidate host paths to the subprocess stdin, one
// per line, and reads the PROTECTED subset back from stdout, one per line. The
// load-bearing property is byte fidelity: a path is the key both sides match on,
// so leading/trailing/interior SPACES and exotic characters must survive the
// round trip intact — if "/models/My Model " came back as "/models/My Model"
// the interlock would fail to match it and a live model would look evictable.
// Empty lines are not paths and must be dropped on both sides.
//
// The real llm-serve liveness probe needs Docker, so here a faithful stand-in
// script implements the same wire contract (echo back the protected lines). This
// drives the REAL interlock.LLMServeChecker across a REAL subprocess pipe; the
// serve-side parser (readLines) and tidy-side parser (splitNonEmpty) are unit-
// tested in their own packages. The full end-to-end against a live llm-serve +
// Docker is the build-tagged integration deferral (see the audit).
func TestSeam_InterlockWireProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stand-in not available on Windows")
	}

	// Stand-in for `llm-serve liveness --check`: read candidate paths from stdin,
	// echo back the ones marked protected (contain "KEEP"), preserving bytes.
	script := filepath.Join(t.TempDir(), "fake-llm-serve")
	body := "#!/bin/sh\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    *KEEP*) printf '%s\\n' \"$line\" ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	candidates := []string{
		"/models/KEEP-alpha",
		"/models/free-to-evict",
		"/models/KEEP beta with spaces",   // interior spaces
		"/models/KEEP-trailing-space ",    // trailing space — the dangerous case
		"  /models/KEEP-leading-space",    // leading space
	}

	protected, _, err := interlock.LLMServeChecker(script)(context.Background(), candidates)
	if err != nil {
		t.Fatalf("interlock shell-out across the wire failed: %v", err)
	}

	want := []string{
		"/models/KEEP-alpha",
		"/models/KEEP beta with spaces",
		"/models/KEEP-trailing-space ",
		"  /models/KEEP-leading-space",
	}
	slices.Sort(protected)
	slices.Sort(want)
	if !slices.Equal(protected, want) {
		t.Errorf("SEAM CONTRACT BROKEN: protected paths did not round-trip byte-for-byte\n got: %q\nwant: %q", protected, want)
	}

	// The evictable path must NOT come back protected.
	if slices.Contains(protected, "/models/free-to-evict") {
		t.Error("an unprotected candidate must not be reported protected")
	}
}
