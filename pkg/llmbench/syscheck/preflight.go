package syscheck

import (
	"context"
	"fmt"
)

// DirtyMode controls how pre-flight check failures are handled.
type DirtyMode string

const (
	DirtyModeAbort DirtyMode = "abort" // Abort on resource issues, warn on non-ideal conditions
	DirtyModeWarn  DirtyMode = "warn"  // Warn on everything, abort on nothing
	DirtyModeForce DirtyMode = "force" // Skip all checks
)

// ParseDirtyMode parses a string into a DirtyMode.
func ParseDirtyMode(s string) (DirtyMode, error) {
	switch s {
	case "", "abort":
		return DirtyModeAbort, nil
	case "warn":
		return DirtyModeWarn, nil
	case "force":
		return DirtyModeForce, nil
	default:
		return "", fmt.Errorf("invalid dirty_mode: %q (valid: abort, warn, force)", s)
	}
}

// CheckResult holds the outcome of a single pre-flight check.
type CheckResult struct {
	Name    string // Check name (e.g. "idle", "thermal", "resources")
	Failed  bool   // Whether the check failed
	Message string // Human-readable result message
	Warning string // Non-fatal warning message
}

// PreflightResult holds the results of all pre-flight checks.
type PreflightResult struct {
	Passed   bool
	Results  []CheckResult
	Warnings []string
}

// RunPreflight executes all pre-flight checks according to the dirty mode.
func RunPreflight(ctx context.Context, mode DirtyMode, resultDir string) PreflightResult {
	if mode == DirtyModeForce {
		return PreflightResult{
			Passed:   true,
			Warnings: []string{"Pre-flight checks skipped (dirty_mode: force)"},
		}
	}

	checks := []func(context.Context) CheckResult{
		DefaultIdleCheck().Run,
		DefaultThermalCheck().Run,
		func(ctx context.Context) CheckResult {
			rc := DefaultResourceCheck()
			rc.ResultDir = resultDir
			return rc.Run(ctx)
		},
	}

	var results []CheckResult
	var warnings []string
	passed := true

	for _, check := range checks {
		r := check(ctx)
		results = append(results, r)
		if r.Warning != "" {
			warnings = append(warnings, r.Warning)
		}
		if r.Failed {
			if mode == DirtyModeAbort {
				passed = false
			} else {
				// warn mode: record as warning, don't fail
				warnings = append(warnings, r.Message)
				r.Failed = false
			}
		}
	}

	return PreflightResult{
		Passed:   passed,
		Results:  results,
		Warnings: warnings,
	}
}
