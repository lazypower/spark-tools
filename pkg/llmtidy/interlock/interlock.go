// Package interlock is the llm-tidy ↔ llm-serve eviction safety gate (B3): before
// llm-tidy deletes a model's files, it asks llm-serve's liveness authority whether
// that path is protected (a running container is using it, or it's a desired
// instance). A protected path is never pruned. The check is a CLI shell-out to
// `llm-serve liveness --check`, so llm-tidy depends only on llm-serve's stable CLI
// contract — not its internals (an extract-a-shared-surface refactor is parked for
// later; this is the light path).
//
// Fail-closed: if liveness is present but cannot be determined, every path-based
// candidate is blocked. If llm-serve is ABSENT (not deployed on this box), the
// interlock is inactive — there are no llm-serve-served models to protect.
// Ollama models (no on-disk path) are never gated here; Ollama owns their runtime.
package interlock

import (
	"context"
	"errors"

	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

// InstalledModel is the prune candidate (re-aliased to avoid an import in callers).
type InstalledModel = inventory.InstalledModel

// ErrLLMServeAbsent signals that the llm-serve binary is not available, so the
// interlock cannot run — and, by design, does not need to (no llm-serve here).
var ErrLLMServeAbsent = errors.New("llm-serve not found; eviction interlock inactive")

// Checker returns the subset of candidate host paths that llm-serve reports
// PROTECTED, plus any complaint warnings (e.g. an unmanaged container holding a
// model store). It returns ErrLLMServeAbsent when llm-serve is not installed; any
// other error means liveness could not be determined → the caller fails closed.
type Checker func(ctx context.Context, paths []string) (protected []string, warnings []string, err error)

// Blocked is a candidate the interlock refused to prune, with why.
type Blocked struct {
	Model  InstalledModel
	Reason string
}

// Result partitions a prune plan after the interlock.
type Result struct {
	Keep     []InstalledModel // safe to prune
	Blocked  []Blocked        // protected (or fail-closed) — must not be pruned
	Warnings []string         // complaints to surface (unmanaged containers, etc.)
	Inactive bool             // llm-serve absent — interlock did not run
}

// Apply gates a prune plan against llm-serve liveness. Candidates with no on-disk
// Path (Ollama) pass through unchecked. Path-based candidates that liveness
// reports protected are blocked; if the check errors (and llm-serve is present),
// ALL path-based candidates are blocked (fail-closed). If llm-serve is absent the
// plan passes through with Inactive=true.
func Apply(ctx context.Context, plan []InstalledModel, check Checker) Result {
	var res Result
	var pathBased []InstalledModel
	var paths []string
	for _, m := range plan {
		if m.Path == "" {
			res.Keep = append(res.Keep, m) // not path-based (Ollama) — never gated here
			continue
		}
		pathBased = append(pathBased, m)
		paths = append(paths, m.Path)
	}
	if len(pathBased) == 0 {
		return res
	}

	protected, warnings, err := check(ctx, paths)
	res.Warnings = warnings
	switch {
	case errors.Is(err, ErrLLMServeAbsent):
		res.Inactive = true
		res.Keep = append(res.Keep, pathBased...) // no llm-serve here → nothing to protect
		return res
	case err != nil:
		// Liveness present but undeterminable → fail closed: block everything path-based.
		for _, m := range pathBased {
			res.Blocked = append(res.Blocked, Blocked{Model: m, Reason: "llm-serve liveness unavailable (fail-closed): " + err.Error()})
		}
		return res
	}

	protectedSet := make(map[string]bool, len(protected))
	for _, p := range protected {
		protectedSet[p] = true
	}
	for _, m := range pathBased {
		if protectedSet[m.Path] {
			res.Blocked = append(res.Blocked, Blocked{Model: m, Reason: "in use / protected by llm-serve"})
		} else {
			res.Keep = append(res.Keep, m)
		}
	}
	return res
}
