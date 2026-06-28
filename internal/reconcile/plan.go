package reconcile

import (
	"time"

	"github.com/lazypower/spark-tools/internal/inventory"
)

// PruneOptions filters which untracked models become prune candidates.
type PruneOptions struct {
	// Backend, if non-nil, restricts pruning to one backend.
	Backend *inventory.ModelBackend

	// OlderThan, if non-zero, only prunes untracked models last modified
	// before now-OlderThan. Implements --older-than from spec §7.1.
	OlderThan time.Duration
}

// PrunePlan returns the untracked models that should be removed under the
// given options.
func PrunePlan(d DiffResult, opts PruneOptions, now time.Time) []inventory.InstalledModel {
	cutoff := time.Time{}
	if opts.OlderThan > 0 {
		cutoff = now.Add(-opts.OlderThan)
	}
	var plan []inventory.InstalledModel
	for _, m := range d.Untracked {
		if opts.Backend != nil && m.Backend != *opts.Backend {
			continue
		}
		if !cutoff.IsZero() && !m.Modified.Before(cutoff) {
			continue
		}
		plan = append(plan, m)
	}
	return plan
}

// SyncOptions filters which missing specs become sync candidates.
type SyncOptions struct {
	Backend *inventory.ModelBackend
}

// SyncPlan returns the manifest specs whose models are missing and should
// be pulled.
func SyncPlan(d DiffResult, opts SyncOptions) []ModelSpec {
	var plan []ModelSpec
	for _, s := range d.Missing {
		if opts.Backend != nil && s.Backend != *opts.Backend {
			continue
		}
		plan = append(plan, s)
	}
	return plan
}

// TotalSize sums the byte sizes of a list of installed models.
func TotalSize(models []inventory.InstalledModel) int64 {
	var total int64
	for _, m := range models {
		total += m.Size
	}
	return total
}
