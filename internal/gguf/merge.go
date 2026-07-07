package gguf

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type shardInfo struct {
	header   *ShardHeader
	path     string
	dataSize uint64 // file size minus data offset
}

// MergeShards combines multiple split GGUF shard files into a single GGUF.
// Shard paths must be sorted in order (00001, 00002, ...). Metadata KVs are
// taken from the first shard; split.* keys are stripped. Tensor data sections
// are concatenated with alignment padding between them.
func MergeShards(shardPaths []string, outputPath string) error {
	if len(shardPaths) < 2 {
		return fmt.Errorf("need at least 2 shards to merge")
	}

	sort.Strings(shardPaths)

	shards, err := parseAllShards(shardPaths)
	if err != nil {
		return err
	}

	// Refuse to merge an incomplete shard set. Without this a missing shard
	// merges "successfully" into a truncated model that looks fine on disk.
	if err := validateShardSet(shards); err != nil {
		return err
	}

	alignment := shards[0].header.Alignment

	// Build merged KVs from shard 0, stripping split-tracking keys.
	var kvs []KV
	for _, kv := range shards[0].header.KVs {
		if strings.HasPrefix(kv.Key, "split.") {
			continue
		}
		kvs = append(kvs, kv)
	}

	// Calculate cumulative data offset for each shard's data section
	// in the merged output. Pad between shards to maintain alignment.
	cumulativeOffset := make([]uint64, len(shards))
	running := uint64(0)
	for i := range shards {
		cumulativeOffset[i] = running
		running = alignUp(running+shards[i].dataSize, alignment)
	}

	// Collect all tensor infos, adjusting offsets for the merged layout.
	var tensors []TensorInfo
	var totalTensors uint64
	for i, s := range shards {
		for _, t := range s.header.Tensors {
			t.Offset += cumulativeOffset[i]
			tensors = append(tensors, t)
		}
		totalTensors += s.header.TensorCount
	}

	// Write atomically. A merge is multi-GB and can be interrupted (crash, full
	// disk, Ctrl-C); writing straight to outputPath would leave a truncated file
	// that the next ollama-import reuses via a size>0 check. Stage in a temp file
	// in the same directory, fsync, and rename into place so outputPath only ever
	// appears as a complete merge.
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".merge-*.gguf.tmp")
	if err != nil {
		return fmt.Errorf("creating temp output: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := writeMergedBody(tmp, shards, kvs, tensors, totalTensors, alignment); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing merged output: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing merged output: %w", err)
	}
	if err := os.Rename(tmpName, outputPath); err != nil {
		return fmt.Errorf("renaming merged output into place: %w", err)
	}
	return nil
}

// writeMergedBody writes the merged header, alignment padding, and every shard's
// tensor data into out.
func writeMergedBody(out *os.File, shards []shardInfo, kvs []KV, tensors []TensorInfo, totalTensors, alignment uint64) error {
	if err := writeMergedHeader(out, shards[0].header.Version, totalTensors, kvs, tensors); err != nil {
		return err
	}
	// Pad header to alignment boundary before data section.
	if err := padToAlignment(out, alignment); err != nil {
		return err
	}
	// Copy tensor data sections from each shard.
	for i, s := range shards {
		if err := copyShardData(out, s, alignment, i == len(shards)-1); err != nil {
			return fmt.Errorf("copying shard %d data: %w", i, err)
		}
	}
	return nil
}

// validateShardSet ensures the provided shards form a complete split set. GGUF
// split files declare split.count (total shards) and split.no (this shard's
// 0-based index); if either is absent the shards predate the convention and are
// accepted as-is. When present, the count must match the number of shards given
// and every index must be distinct and in range — a missing or duplicated shard
// is rejected rather than silently merged into a truncated model.
func validateShardSet(shards []shardInfo) error {
	count := shardKVUint(shards[0], "split.count")
	if count == 0 {
		return nil // not a declared split set; nothing to validate against
	}
	if count != uint64(len(shards)) {
		return fmt.Errorf("incomplete shard set: split.count is %d but %d shard(s) were provided", count, len(shards))
	}
	seen := make(map[uint64]bool, len(shards))
	for _, s := range shards {
		no := shardKVUint(s, "split.no")
		if no >= count {
			return fmt.Errorf("shard %s declares split.no %d, out of range for split.count %d", s.path, no, count)
		}
		if seen[no] {
			return fmt.Errorf("duplicate shard index %d (shard %s)", no, s.path)
		}
		seen[no] = true
	}
	return nil
}

// shardKVUint returns the unsigned integer value of a shard header KV, or 0 when
// the key is absent.
func shardKVUint(s shardInfo, key string) uint64 {
	for _, kv := range s.header.KVs {
		if kv.Key == key {
			return asUint64(kv.Value)
		}
	}
	return 0
}

func parseAllShards(paths []string) ([]shardInfo, error) {
	shards := make([]shardInfo, 0, len(paths))
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("opening shard %s: %w", p, err)
		}

		hdr, err := ParseShard(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("parsing shard %s: %w", p, err)
		}

		fi, err := f.Stat()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("stat shard %s: %w", p, err)
		}

		shards = append(shards, shardInfo{
			header:   hdr,
			path:     p,
			dataSize: uint64(fi.Size()) - hdr.DataOffset,
		})
	}
	return shards, nil
}

func writeMergedHeader(w io.Writer, version uint32, tensorCount uint64, kvs []KV, tensors []TensorInfo) error {
	if err := writeGGUFHeader(w, version, tensorCount, uint64(len(kvs))); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	for _, kv := range kvs {
		if err := writeKVBin(w, kv); err != nil {
			return fmt.Errorf("writing kv %q: %w", kv.Key, err)
		}
	}
	for _, t := range tensors {
		if err := writeTensorInfoBin(w, t); err != nil {
			return fmt.Errorf("writing tensor info %q: %w", t.Name, err)
		}
	}
	return nil
}

func padToAlignment(f *os.File, alignment uint64) error {
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	padded := alignUp(uint64(pos), alignment)
	if padded > uint64(pos) {
		if _, err := f.Write(make([]byte, padded-uint64(pos))); err != nil {
			return err
		}
	}
	return nil
}

func copyShardData(out *os.File, s shardInfo, alignment uint64, isLast bool) error {
	sf, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer sf.Close()

	if _, err := sf.Seek(int64(s.header.DataOffset), io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(out, sf, int64(s.dataSize)); err != nil {
		return err
	}

	// Pad between shards to maintain alignment (skip for last shard).
	if !isLast {
		padSize := alignUp(s.dataSize, alignment) - s.dataSize
		if padSize > 0 {
			if _, err := out.Write(make([]byte, padSize)); err != nil {
				return err
			}
		}
	}

	return nil
}
