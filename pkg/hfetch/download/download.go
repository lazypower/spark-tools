// Package download is a compatibility wrapper over internal/download. The
// resumable chunked downloader (range fallback, rate limiting, disk-space checks,
// resumable chunk state, SHA-256 verification) moved to internal/download during
// the /internal extraction; this thin alias keeps existing importers
// (cmd/hfetch, pkg/hfetch, pkg/hfetch/source) compiling unchanged until they
// migrate. Type aliases carry the ChunkState methods over; ErrRangeNotSupported
// is re-exported by identity so errors.Is/== still match across the boundary.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/download.
package download

import (
	"context"

	dl "github.com/lazypower/spark-tools/internal/download"
)

// Type aliases — carry the ChunkState methods over and keep values (incl. the
// FileSource interface and Options/ProgressEvent structs) flowing across the
// boundary as the same type.
type (
	ChunkState    = dl.ChunkState
	ByteRange     = dl.ByteRange
	ProgressEvent = dl.ProgressEvent
	ProgressFunc  = dl.ProgressFunc
	FileSource    = dl.FileSource
	Options       = dl.Options
)

// ErrRangeNotSupported is re-exported by identity (same var) so callers'
// errors.Is/== checks against the wrapper and the authority both succeed.
var ErrRangeNotSupported = dl.ErrRangeNotSupported

// LoadState reads resumable chunk state from path.
func LoadState(path string) (*ChunkState, error) { return dl.LoadState(path) }

// SaveState persists resumable chunk state to path.
func SaveState(path string, state *ChunkState) error { return dl.SaveState(path, state) }

// Download fetches src into filename, resuming/parallelizing per opts.
func Download(ctx context.Context, src FileSource, filename string, opts Options) (string, error) {
	return dl.Download(ctx, src, filename, opts)
}

// VerifySHA256 checks that the file at path hashes to expected.
func VerifySHA256(path, expected string) error { return dl.VerifySHA256(path, expected) }
