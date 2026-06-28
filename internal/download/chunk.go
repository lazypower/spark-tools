package download

import (
	"encoding/json"
	"os"
)

const defaultChunkSize int64 = 64 * 1024 * 1024 // 64 MB

// ChunkState tracks completed byte ranges for resumable downloads.
type ChunkState struct {
	TotalSize int64      `json:"total_size"`
	SHA256    string     `json:"sha256,omitempty"`
	Chunks    []ByteRange `json:"chunks"`
}

// ByteRange represents a completed byte range.
type ByteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // inclusive
}

// CompletedBytes returns the total number of downloaded bytes.
func (s *ChunkState) CompletedBytes() int64 {
	var total int64
	for _, c := range s.Chunks {
		total += c.End - c.Start + 1
	}
	return total
}

// IsComplete returns true if all bytes have been downloaded.
func (s *ChunkState) IsComplete() bool {
	return s.TotalSize > 0 && s.CompletedBytes() >= s.TotalSize
}

// NextOffset returns the byte offset to resume downloading from.
// Returns the end of the last contiguous chunk from byte 0.
func (s *ChunkState) NextOffset() int64 {
	if len(s.Chunks) == 0 {
		return 0
	}

	// Find the contiguous range starting from 0.
	var offset int64
	for _, c := range s.Chunks {
		if c.Start <= offset {
			if c.End+1 > offset {
				offset = c.End + 1
			}
		} else {
			break
		}
	}
	return offset
}

// AddChunk records a completed byte range.
func (s *ChunkState) AddChunk(start, end int64) {
	s.Chunks = append(s.Chunks, ByteRange{Start: start, End: end})
	s.merge()
}

// merge consolidates overlapping/adjacent byte ranges.
func (s *ChunkState) merge() {
	if len(s.Chunks) <= 1 {
		return
	}

	// Sort by start.
	for i := 1; i < len(s.Chunks); i++ {
		for j := i; j > 0 && s.Chunks[j].Start < s.Chunks[j-1].Start; j-- {
			s.Chunks[j], s.Chunks[j-1] = s.Chunks[j-1], s.Chunks[j]
		}
	}

	merged := []ByteRange{s.Chunks[0]}
	for _, c := range s.Chunks[1:] {
		last := &merged[len(merged)-1]
		if c.Start <= last.End+1 {
			if c.End > last.End {
				last.End = c.End
			}
		} else {
			merged = append(merged, c)
		}
	}
	s.Chunks = merged
}

// LoadState reads chunk state from a .state sidecar file.
func LoadState(path string) (*ChunkState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ChunkState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state ChunkState
	if err := json.Unmarshal(data, &state); err != nil {
		return &ChunkState{}, nil // corrupt state = start fresh
	}
	return &state, nil
}

// SaveState writes chunk state to a .state sidecar file.
func SaveState(path string, state *ChunkState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
