// Package download implements resumable, parallel downloads with
// SHA256 verification and atomic finalization.
package download

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressEvent reports download progress.
type ProgressEvent struct {
	File           string
	BytesCompleted int64
	BytesTotal     int64
	Speed          float64 // bytes per second
	Phase          string  // "downloading", "verifying", "complete"
}

// ProgressFunc is a callback for progress updates.
type ProgressFunc func(ProgressEvent)

// FileSource provides file data. Abstracted for testing.
type FileSource interface {
	// Download opens a stream starting at offset. Returns the reader and content length.
	Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error)
	// Head returns the total file size and SHA256 hash.
	Head(ctx context.Context) (size int64, sha256 string, err error)
}

// Options configures a download.
type Options struct {
	OutputDir    string
	Streams      int   // Parallel download streams (default: 4)
	ChunkSize    int64
	MaxRetries   int
	MaxBandwidth int64 // bytes per second, 0 = unlimited
	OnProgress   ProgressFunc

	bucket *tokenBucket // shared rate limiter, set internally
}

func (o *Options) streams() int {
	if o.Streams > 0 {
		return o.Streams
	}
	return 4
}

func (o *Options) chunkSize() int64 {
	if o.ChunkSize > 0 {
		return o.ChunkSize
	}
	return defaultChunkSize
}

func (o *Options) maxRetries() int {
	if o.MaxRetries > 0 {
		return o.MaxRetries
	}
	return 5
}

// Download downloads a file using the given source with resume support
// and parallel streams.
// Returns the final path of the completed file.
//
// The download follows an atomic finalization protocol:
//  1. Data writes to <filename>.partial
//  2. State tracked in <filename>.state
//  3. SHA256 verified on completion
//  4. fsync + rename to final path
//  5. State file removed
func Download(ctx context.Context, src FileSource, filename string, opts Options) (string, error) {
	if err := os.MkdirAll(opts.OutputDir, 0700); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}

	finalPath := filepath.Join(opts.OutputDir, filename)
	partialPath := finalPath + ".partial"
	statePath := finalPath + ".state"

	// If final file already exists, we're done.
	if info, err := os.Stat(finalPath); err == nil && info.Size() > 0 {
		return finalPath, nil
	}

	// Get file metadata.
	totalSize, expectedHash, err := src.Head(ctx)
	if err != nil {
		return "", fmt.Errorf("HEAD request: %w", err)
	}

	// Disk space pre-check.
	if err := checkDiskSpace(opts.OutputDir, totalSize); err != nil {
		return "", err
	}

	// Load or create chunk state.
	state, err := LoadState(statePath)
	if err != nil {
		return "", fmt.Errorf("loading state: %w", err)
	}
	state.TotalSize = totalSize
	state.SHA256 = expectedHash

	// Pre-allocate the partial file to totalSize.
	f, err := os.OpenFile(partialPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return "", fmt.Errorf("opening partial file: %w", err)
	}
	if info, _ := f.Stat(); info.Size() < totalSize {
		if err := f.Truncate(totalSize); err != nil {
			f.Close()
			return "", fmt.Errorf("pre-allocating file: %w", err)
		}
	}
	f.Close()

	// Build work items: divide remaining bytes into chunks.
	chunkSize := opts.chunkSize()
	var work []chunkWork
	for offset := int64(0); offset < totalSize; offset += chunkSize {
		end := offset + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		// Skip already-completed chunks.
		if state.isRangeDone(offset, end) {
			continue
		}
		work = append(work, chunkWork{start: offset, end: end})
	}

	if len(work) == 0 {
		// All chunks done — skip to verification.
		goto verify
	}

	// Set up bandwidth throttling if configured.
	if opts.MaxBandwidth > 0 {
		opts.bucket = newTokenBucket(opts.MaxBandwidth)
	}

	// Parallel download.
	{
		startTime := time.Now()
		baseCompleted := state.CompletedBytes()
		var newBytes atomic.Int64

		numStreams := opts.streams()
		if numStreams > len(work) {
			numStreams = len(work)
		}

		workCh := make(chan chunkWork, len(work))
		for _, w := range work {
			workCh <- w
		}
		close(workCh)

		var stateMu sync.Mutex
		var wg sync.WaitGroup
		errCh := make(chan error, numStreams)

		dctx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)

		for range numStreams {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for chunk := range workCh {
					if err := downloadChunk(dctx, src, partialPath, chunk, opts.maxRetries(), opts.bucket, &stateMu, state, statePath); err != nil {
						errCh <- err
						cancel(err)
						return
					}
					chunkBytes := chunk.end - chunk.start + 1
					newBytes.Add(chunkBytes)

					if opts.OnProgress != nil {
						completed := baseCompleted + newBytes.Load()
						elapsed := time.Since(startTime).Seconds()
						speed := float64(newBytes.Load()) / max(elapsed, 0.001)
						opts.OnProgress(ProgressEvent{
							File:           filename,
							BytesCompleted: completed,
							BytesTotal:     totalSize,
							Speed:          speed,
							Phase:          "downloading",
						})
					}
				}
			}()
		}

		wg.Wait()

		select {
		case err := <-errCh:
			stateMu.Lock()
			SaveState(statePath, state)
			stateMu.Unlock()
			return "", err
		default:
		}

		// Final state save.
		stateMu.Lock()
		SaveState(statePath, state)
		stateMu.Unlock()
	}

verify:
	// Verify SHA256.
	if opts.OnProgress != nil {
		opts.OnProgress(ProgressEvent{
			File:           filename,
			BytesCompleted: totalSize,
			BytesTotal:     totalSize,
			Phase:          "verifying",
		})
	}

	if err := VerifySHA256(partialPath, expectedHash); err != nil {
		return "", err
	}

	// Fsync before rename.
	ff, err := os.OpenFile(partialPath, os.O_RDWR, 0644)
	if err == nil {
		ff.Sync()
		ff.Close()
	}

	// Atomic rename.
	if err := os.Rename(partialPath, finalPath); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}

	// Clean up state file.
	os.Remove(statePath)

	if opts.OnProgress != nil {
		opts.OnProgress(ProgressEvent{
			File:           filename,
			BytesCompleted: totalSize,
			BytesTotal:     totalSize,
			Phase:          "complete",
		})
	}

	return finalPath, nil
}

type chunkWork struct {
	start, end int64 // inclusive byte range
}

// downloadChunk downloads a single byte range and writes it to the file at the correct offset.
func downloadChunk(ctx context.Context, src FileSource, partialPath string, chunk chunkWork, maxRetries int, bucket *tokenBucket, stateMu *sync.Mutex, state *ChunkState, statePath string) error {
	var lastErr error
	for retry := range maxRetries {
		_ = retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body, _, err := src.Download(ctx, chunk.start)
		if err != nil {
			lastErr = err
			continue
		}

		toRead := chunk.end - chunk.start + 1

		// Wrap the reader with rate limiting if configured.
		var reader io.Reader = io.LimitReader(body, toRead)
		if bucket != nil {
			reader = &rateLimitedReader{r: reader, bucket: bucket}
		}

		data, err := io.ReadAll(reader)
		body.Close()

		if int64(len(data)) > 0 {
			// Write at the correct offset in the file (pwrite-style).
			wf, err := os.OpenFile(partialPath, os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("opening partial for write: %w", err)
			}
			_, writeErr := wf.WriteAt(data, chunk.start)
			wf.Close()
			if writeErr != nil {
				return fmt.Errorf("writing chunk at offset %d: %w", chunk.start, writeErr)
			}

			stateMu.Lock()
			state.AddChunk(chunk.start, chunk.start+int64(len(data))-1)
			SaveState(statePath, state)
			stateMu.Unlock()
		}

		if err != nil {
			lastErr = err
			continue
		}

		if int64(len(data)) < toRead {
			lastErr = fmt.Errorf("short read: got %d bytes, expected %d", len(data), toRead)
			continue
		}

		return nil
	}
	return fmt.Errorf("chunk %d-%d failed after %d retries: %w", chunk.start, chunk.end, maxRetries, lastErr)
}

// isRangeDone checks if a byte range is fully covered by completed chunks.
func (s *ChunkState) isRangeDone(start, end int64) bool {
	for _, c := range s.Chunks {
		if c.Start <= start && c.End >= end {
			return true
		}
	}
	return false
}

// checkDiskSpace verifies sufficient free space before downloading.
// Best-effort: if we can't determine free space, proceed anyway.
func checkDiskSpace(dir string, needed int64) error {
	free, err := freeDiskSpace(dir)
	if err != nil {
		return nil // can't check, proceed
	}
	if free < needed {
		return fmt.Errorf("insufficient disk space: need %d bytes but only %d available in %s", needed, free, dir)
	}
	return nil
}
