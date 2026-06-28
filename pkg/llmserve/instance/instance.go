// Package instance is a compatibility wrapper over internal/serveinstance. The
// authority over a managed serving instance's on-disk manifest state (slice B1
// of llm-serve v2) moved to internal/serveinstance during the /internal
// extraction; this thin alias keeps existing importers
// (pkg/llmserve/{lifecycle,liveness,plan,llmserve}, cmd/llm-serve, pkg/seam)
// compiling unchanged until they migrate. Type aliases carry the Store and
// Instance methods over; ErrNotFound is re-exported by identity so
// errors.Is/== comparisons across the boundary still match.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/serveinstance.
package instance

import (
	si "github.com/lazypower/spark-tools/internal/serveinstance"
)

// Type aliases — carry methods (Store.Save/Load/List/Delete/Lock, Phase.Valid,
// Instance.InFlight) over unchanged and keep values flowing across the boundary
// as the same type.
type (
	Desired   = si.Desired
	Phase     = si.Phase
	Operation = si.Operation
	Instance  = si.Instance
	Store     = si.Store
)

// ErrNotFound is re-exported by identity (same var) so callers' errors.Is/==
// checks against the wrapper and the authority both succeed.
var ErrNotFound = si.ErrNotFound

// Phase enum (non-terminal operation phases).
const (
	PhaseStarting        = si.PhaseStarting
	PhaseReplacing       = si.PhaseReplacing
	PhaseStopping        = si.PhaseStopping
	PhaseCleanupRequired = si.PhaseCleanupRequired
)

// NewStore returns a Store backed by dir (created on first write).
func NewStore(dir string) *Store { return si.NewStore(dir) }

// ValidName reports whether name is safe to use as a manifest filename.
func ValidName(name string) bool { return si.ValidName(name) }
