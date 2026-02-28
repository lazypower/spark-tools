package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ---------- Token bucket and rate limiter tests ----------

func TestNewTokenBucket(t *testing.T) {
	tb := newTokenBucket(1000)
	if tb.rate != 1000 {
		t.Errorf("expected rate 1000, got %f", tb.rate)
	}
	if tb.capacity != 1000 {
		t.Errorf("expected capacity 1000, got %f", tb.capacity)
	}
	// Starts with one second of burst (tokens == rate).
	if tb.tokens != 1000 {
		t.Errorf("expected initial tokens 1000, got %f", tb.tokens)
	}
}

func TestTokenBucketTakeImmediate(t *testing.T) {
	// Bucket with 1000 bytes/sec starts with 1000 tokens.
	tb := newTokenBucket(1000)

	// Taking less than available tokens should return immediately.
	got := tb.take(500)
	if got != 500 {
		t.Errorf("expected to take 500, got %d", got)
	}

	// 500 tokens remain; taking 500 more should also work.
	got = tb.take(500)
	if got != 500 {
		t.Errorf("expected to take 500, got %d", got)
	}
}

func TestTokenBucketTakeClampedToCapacity(t *testing.T) {
	// Capacity is 100 (rate=100). Asking for 500 should clamp to 100.
	tb := newTokenBucket(100)

	got := tb.take(500)
	if got != 100 {
		t.Errorf("expected take clamped to capacity 100, got %d", got)
	}
}

func TestTokenBucketTakeBlocks(t *testing.T) {
	// Create a bucket with 100 bytes/sec, then drain it.
	tb := newTokenBucket(100)
	tb.take(100) // drain all tokens

	// Next take should block until tokens refill.
	start := time.Now()
	got := tb.take(50)
	elapsed := time.Since(start)

	if got != 50 {
		t.Errorf("expected 50, got %d", got)
	}
	// Should have waited approximately 0.5 seconds (50 tokens / 100 per sec).
	if elapsed < 400*time.Millisecond {
		t.Errorf("expected blocking for ~500ms, but only waited %v", elapsed)
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := newTokenBucket(1000)
	tb.take(1000) // drain

	// Manually advance lastFill to simulate time passing.
	tb.mu.Lock()
	tb.lastFill = time.Now().Add(-1 * time.Second)
	tb.refill()
	tokens := tb.tokens
	tb.mu.Unlock()

	// After 1 second, should have refilled to capacity (1000).
	if tokens < 999 {
		t.Errorf("expected ~1000 tokens after 1s refill, got %f", tokens)
	}
}

func TestTokenBucketRefillCapsAtCapacity(t *testing.T) {
	tb := newTokenBucket(500)

	// Simulate a long time passing — tokens should cap at capacity.
	tb.mu.Lock()
	tb.lastFill = time.Now().Add(-10 * time.Second)
	tb.refill()
	tokens := tb.tokens
	tb.mu.Unlock()

	if tokens != 500 {
		t.Errorf("expected tokens capped at capacity 500, got %f", tokens)
	}
}

func TestRateLimitedReaderThrottles(t *testing.T) {
	// Create 500 bytes of data with a rate limit of 1000 bytes/sec.
	data := bytes.Repeat([]byte("x"), 500)
	bucket := newTokenBucket(1000)

	// Drain the bucket so the reader must wait for refills.
	bucket.take(1000)

	reader := &rateLimitedReader{
		r:      bytes.NewReader(data),
		bucket: bucket,
	}

	start := time.Now()
	buf := make([]byte, 500)
	totalRead := 0
	for totalRead < 500 {
		n, err := reader.Read(buf[totalRead:])
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if totalRead != 500 {
		t.Errorf("expected to read 500 bytes, got %d", totalRead)
	}
	// At 1000 bytes/sec with 500 bytes, should take ~500ms.
	if elapsed < 400*time.Millisecond {
		t.Errorf("expected throttling to take ~500ms, got %v", elapsed)
	}
}

func TestRateLimitedReaderPassesData(t *testing.T) {
	data := []byte("hello, rate-limited world!")
	bucket := newTokenBucket(10000) // high rate so it doesn't slow down

	reader := &rateLimitedReader{
		r:      bytes.NewReader(data),
		bucket: bucket,
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: expected %q, got %q", data, got)
	}
}

func TestRateLimitedReaderEOF(t *testing.T) {
	// Empty reader should return EOF immediately.
	bucket := newTokenBucket(1000)
	reader := &rateLimitedReader{
		r:      bytes.NewReader(nil),
		bucket: bucket,
	}

	buf := make([]byte, 100)
	n, err := reader.Read(buf)
	if n != 0 || err != io.EOF {
		t.Errorf("expected 0 bytes and EOF, got n=%d err=%v", n, err)
	}
}

// ---------- isRangeDone tests ----------

func TestIsRangeDoneNoChunks(t *testing.T) {
	s := &ChunkState{TotalSize: 100}
	if s.isRangeDone(0, 99) {
		t.Error("empty state should not report range done")
	}
}

func TestIsRangeDoneFullyCovered(t *testing.T) {
	s := &ChunkState{TotalSize: 100}
	s.AddChunk(0, 99)
	if !s.isRangeDone(0, 99) {
		t.Error("full chunk should cover entire range")
	}
	if !s.isRangeDone(10, 50) {
		t.Error("sub-range should be covered")
	}
}

func TestIsRangeDonePartialCoverage(t *testing.T) {
	s := &ChunkState{TotalSize: 100}
	s.AddChunk(0, 49)

	if s.isRangeDone(0, 99) {
		t.Error("partial chunk should not cover full range")
	}
	if !s.isRangeDone(0, 49) {
		t.Error("range within chunk should be covered")
	}
	if s.isRangeDone(25, 75) {
		t.Error("range extending beyond chunk should not be covered")
	}
}

func TestIsRangeDoneMultipleChunks(t *testing.T) {
	s := &ChunkState{TotalSize: 100}
	s.AddChunk(0, 30)
	s.AddChunk(50, 99)

	// Gap from 31-49.
	if s.isRangeDone(0, 99) {
		t.Error("gap should prevent full coverage")
	}
	if !s.isRangeDone(50, 99) {
		t.Error("second chunk should cover its range")
	}
	if s.isRangeDone(25, 55) {
		t.Error("range spanning a gap should not be done")
	}
}

// ---------- maxRetries tests ----------

func TestMaxRetriesDefault(t *testing.T) {
	opts := &Options{}
	if opts.maxRetries() != 5 {
		t.Errorf("expected default maxRetries 5, got %d", opts.maxRetries())
	}
}

func TestMaxRetriesCustom(t *testing.T) {
	opts := &Options{MaxRetries: 10}
	if opts.maxRetries() != 10 {
		t.Errorf("expected maxRetries 10, got %d", opts.maxRetries())
	}
}

func TestMaxRetriesZeroUsesDefault(t *testing.T) {
	opts := &Options{MaxRetries: 0}
	if opts.maxRetries() != 5 {
		t.Errorf("expected default maxRetries 5 for zero, got %d", opts.maxRetries())
	}
}

// ---------- checkDiskSpace tests ----------

func TestCheckDiskSpaceSufficient(t *testing.T) {
	tmp := t.TempDir()
	// 1 byte needed should always pass on a real filesystem.
	if err := checkDiskSpace(tmp, 1); err != nil {
		t.Errorf("expected no error for tiny space need, got: %v", err)
	}
}

func TestCheckDiskSpaceInsufficient(t *testing.T) {
	tmp := t.TempDir()
	// Request an absurdly large amount of space.
	err := checkDiskSpace(tmp, 1<<62)
	if err == nil {
		t.Error("expected error for impossibly large space requirement")
	}
	if err != nil && !strings.Contains(err.Error(), "insufficient disk space") {
		t.Errorf("expected insufficient disk space error, got: %v", err)
	}
}

// ---------- Download with rate limiting integration test ----------

func TestDownloadWithRateLimit(t *testing.T) {
	tmp := t.TempDir()

	data := bytes.Repeat([]byte("rate-limit-test "), 20) // 320 bytes
	src := newMockSource(data)

	path, err := Download(context.Background(), src, "ratelimited.gguf", Options{
		OutputDir:    tmp,
		ChunkSize:    100,
		MaxBandwidth: 100000, // 100KB/s — fast enough not to slow test much
	})
	if err != nil {
		t.Fatalf("Download with rate limit: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: got %d bytes, expected %d", len(got), len(data))
	}
}
