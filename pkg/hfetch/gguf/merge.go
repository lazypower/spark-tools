package gguf

import (
	"fmt"
	"io"
	"os"
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

	// Write the merged output file.
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer out.Close()

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
