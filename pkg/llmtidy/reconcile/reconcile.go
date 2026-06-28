// Package reconcile is a compatibility wrapper over internal/reconcile. The
// manifest-vs-inventory diff and the prune/sync plan+apply logic moved to
// internal/reconcile during the /internal extraction; this thin alias keeps
// existing importers (pkg/llmtidy, cmd/llm-tidy) compiling unchanged until they
// migrate. Type aliases carry the ModelSpec.Name method and the Syncer interface
// over; the diff/plan/apply funcs delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/reconcile.
package reconcile

import (
	"context"
	"time"

	"github.com/lazypower/spark-tools/internal/inventory"
	irec "github.com/lazypower/spark-tools/internal/reconcile"
	"github.com/lazypower/spark-tools/internal/tidymanifest"
)

// Type aliases — carry the ModelSpec.Name method and the Syncer interface over
// and keep values flowing across the boundary as the same type.
type (
	ModelSpec    = irec.ModelSpec
	DiffResult   = irec.DiffResult
	PruneEvent   = irec.PruneEvent
	Syncer       = irec.Syncer
	SyncEvent    = irec.SyncEvent
	PruneOptions = irec.PruneOptions
	SyncOptions  = irec.SyncOptions
)

// Diff compares the manifest against an inventory snapshot.
func Diff(m *tidymanifest.Manifest, installed []inventory.InstalledModel) DiffResult {
	return irec.Diff(m, installed)
}

// Prune removes every model in plan via the provider.
func Prune(
	ctx context.Context,
	p *inventory.Provider,
	plan []inventory.InstalledModel,
	onEvent func(PruneEvent),
) ([]inventory.InstalledModel, int64, error) {
	return irec.Prune(ctx, p, plan, onEvent)
}

// Sync pulls every missing spec via the syncer.
func Sync(ctx context.Context, s Syncer, plan []ModelSpec, onEvent func(SyncEvent)) error {
	return irec.Sync(ctx, s, plan, onEvent)
}

// PrunePlan returns the untracked models that should be removed under opts.
func PrunePlan(d DiffResult, opts PruneOptions, now time.Time) []inventory.InstalledModel {
	return irec.PrunePlan(d, opts, now)
}

// SyncPlan returns the manifest specs whose models are missing.
func SyncPlan(d DiffResult, opts SyncOptions) []ModelSpec {
	return irec.SyncPlan(d, opts)
}

// TotalSize sums the byte sizes of a list of installed models.
func TotalSize(models []inventory.InstalledModel) int64 {
	return irec.TotalSize(models)
}
