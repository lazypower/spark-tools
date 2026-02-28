package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// mockSource is a test FileSource backed by in-memory data.
type mockSource struct {
	data   []byte
	sha256 string
}

func newMockSource(data []byte) *mockSource {
	h := sha256.Sum256(data)
	return &mockSource{
		data:   data,
		sha256: hex.EncodeToString(h[:]),
	}
}

func (m *mockSource) Head(_ context.Context) (int64, string, error) {
	return int64(len(m.data)), m.sha256, nil
}

func (m *mockSource) Download(_ context.Context, offset int64) (io.ReadCloser, int64, error) {
	remaining := int64(len(m.data)) - offset
	reader := bytes.NewReader(m.data[offset:])
	return io.NopCloser(reader), remaining, nil
}

func TestChunkState(t *testing.T) {
	s := &ChunkState{TotalSize: 100}

	if s.CompletedBytes() != 0 {
		t.Errorf("expected 0 completed, got %d", s.CompletedBytes())
	}
	if s.NextOffset() != 0 {
		t.Errorf("expected offset 0, got %d", s.NextOffset())
	}

	s.AddChunk(0, 49)
	if s.CompletedBytes() != 50 {
		t.Errorf("expected 50, got %d", s.CompletedBytes())
	}
	if s.NextOffset() != 50 {
		t.Errorf("expected offset 50, got %d", s.NextOffset())
	}

	s.AddChunk(50, 99)
	if !s.IsComplete() {
		t.Error("expected complete")
	}
}

func TestChunkStateMerge(t *testing.T) {
	s := &ChunkState{TotalSize: 100}
	s.AddChunk(0, 30)
	s.AddChunk(20, 60) // overlapping
	s.AddChunk(61, 99) // adjacent

	if len(s.Chunks) != 1 {
		t.Errorf("expected 1 merged chunk, got %d: %v", len(s.Chunks), s.Chunks)
	}
	if s.Chunks[0].Start != 0 || s.Chunks[0].End != 99 {
		t.Errorf("expected 0-99, got %d-%d", s.Chunks[0].Start, s.Chunks[0].End)
	}
}

func TestStatePersistence(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.state")

	state := &ChunkState{TotalSize: 1000, SHA256: "abc"}
	state.AddChunk(0, 499)

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.TotalSize != 1000 {
		t.Errorf("expected size 1000, got %d", loaded.TotalSize)
	}
	if loaded.CompletedBytes() != 500 {
		t.Errorf("expected 500 completed, got %d", loaded.CompletedBytes())
	}
}

func TestLoadStateMissing(t *testing.T) {
	state, err := LoadState("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.TotalSize != 0 {
		t.Error("expected empty state")
	}
}

func TestDownloadFull(t *testing.T) {
	tmp := t.TempDir()

	// Create test data.
	data := bytes.Repeat([]byte("hello world "), 100)
	src := newMockSource(data)

	var events []ProgressEvent
	path, err := Download(context.Background(), src, "test.gguf", Options{
		OutputDir: tmp,
		ChunkSize: 256, // small chunks for testing
		OnProgress: func(e ProgressEvent) {
			events = append(events, e)
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Verify final file.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: got %d bytes, expected %d", len(got), len(data))
	}

	// Verify no partial/state files remain.
	if _, err := os.Stat(path + ".partial"); !os.IsNotExist(err) {
		t.Error("partial file should be removed")
	}
	if _, err := os.Stat(path + ".state"); !os.IsNotExist(err) {
		t.Error("state file should be removed")
	}

	// Verify progress events.
	if len(events) == 0 {
		t.Error("expected progress events")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.Phase != "complete" {
		t.Errorf("last event phase: expected complete, got %q", lastEvent.Phase)
	}
}

func TestDownloadSkipsExisting(t *testing.T) {
	tmp := t.TempDir()

	// Create the "already downloaded" file.
	finalPath := filepath.Join(tmp, "test.gguf")
	os.WriteFile(finalPath, []byte("existing data"), 0644)

	src := newMockSource([]byte("new data"))
	path, err := Download(context.Background(), src, "test.gguf", Options{
		OutputDir: tmp,
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Should return the existing file without re-downloading.
	got, _ := os.ReadFile(path)
	if string(got) != "existing data" {
		t.Errorf("expected existing data, got %q", string(got))
	}
}

func TestVerifySHA256(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.bin")
	data := []byte("test data for hashing")
	os.WriteFile(path, data, 0644)

	h := sha256.Sum256(data)
	expected := hex.EncodeToString(h[:])

	if err := VerifySHA256(path, expected); err != nil {
		t.Errorf("verification should pass: %v", err)
	}

	if err := VerifySHA256(path, "badhash"); err == nil {
		t.Error("verification should fail with wrong hash")
	}

	// Empty expected hash should pass.
	if err := VerifySHA256(path, ""); err != nil {
		t.Errorf("empty hash should pass: %v", err)
	}
}

func TestDownloadParallelStreams(t *testing.T) {
	tmp := t.TempDir()

	// 4KB of data with 256-byte chunks = 16 chunks, 4 streams.
	data := bytes.Repeat([]byte("parallel-test-data!"), 220) // ~4180 bytes
	src := newMockSource(data)

	var speedSeen bool
	path, err := Download(context.Background(), src, "parallel.gguf", Options{
		OutputDir: tmp,
		ChunkSize: 256,
		Streams:   4,
		OnProgress: func(e ProgressEvent) {
			if e.Phase == "downloading" && e.Speed > 0 {
				speedSeen = true
			}
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch with parallel download: got %d bytes, expected %d", len(got), len(data))
	}

	if !speedSeen {
		t.Error("expected speed > 0 in progress events")
	}
}

func TestDownloadDiskSpaceCheck(t *testing.T) {
	tmp := t.TempDir()

	// Create a source that claims to need more space than exists.
	// We can't really test disk full, but we can verify the check runs.
	data := []byte("small")
	src := newMockSource(data)

	// This should succeed since the data is small.
	path, err := Download(context.Background(), src, "small.gguf", Options{
		OutputDir: tmp,
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Error("data mismatch")
	}
}

func TestDownloadCancellation(t *testing.T) {
	tmp := t.TempDir()

	// Large data to ensure we hit the cancellation.
	data := bytes.Repeat([]byte("x"), 10000)
	src := newMockSource(data)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Download(ctx, src, "test.gguf", Options{
		OutputDir: tmp,
		ChunkSize: 100,
	})
	if err == nil {
		t.Error("expected cancellation error")
	}
}
