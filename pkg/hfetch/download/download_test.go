package download

import (
	"errors"
	"path/filepath"
	"testing"

	dl "github.com/lazypower/spark-tools/internal/download"
)

// The behavior suite (chunked download, range fallback, rate limiting, disk
// space, resume, verify) lives in internal/download; this locks the compat
// surface (alias identity, ChunkState method ride-along, sentinel identity,
// delegated state funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ dl.ChunkState = ChunkState{}
	var _ dl.ByteRange = ByteRange{}
	var _ dl.ProgressEvent = ProgressEvent{}
	var _ dl.Options = Options{}
	var _ dl.FileSource = FileSource(nil)
}

func TestWrapper_SentinelIdentity(t *testing.T) {
	if !errors.Is(ErrRangeNotSupported, dl.ErrRangeNotSupported) {
		t.Fatal("wrapper ErrRangeNotSupported must preserve sentinel identity")
	}
}

func TestWrapper_StateRoundTripAndMethods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := &ChunkState{TotalSize: 100}
	st.AddChunk(0, 49)
	if err := SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	// Aliased ChunkState methods ride along through the wrapper.
	if got.CompletedBytes() != st.CompletedBytes() {
		t.Errorf("round-trip CompletedBytes mismatch: %d vs %d", got.CompletedBytes(), st.CompletedBytes())
	}
}
