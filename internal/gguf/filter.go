package gguf

import (
	"path/filepath"
	"sort"
	"strings"
)

// FileInfo pairs a filename with its parsed quantization for filtering.
type FileInfo struct {
	Filename     string
	Size         int64
	Quantization string
	BitsPerWeight float64
}

// ParseQuantFromFilename attempts to extract the quantization type from
// a GGUF filename (e.g., "model-Q4_K_M.gguf" → "Q4_K_M").
func ParseQuantFromFilename(filename string) string {
	base := strings.TrimSuffix(filepath.Base(filename), ".gguf")
	base = strings.TrimSuffix(base, ".GGUF")

	// Walk from the end looking for a known quant pattern.
	parts := strings.Split(base, "-")
	for i := len(parts) - 1; i >= 0; i-- {
		// Try single part: "Q4_K_M", "Q8_0", "IQ4_XS", "mxfp4"
		candidate := strings.ToUpper(parts[i])
		if _, ok := QuantBitsPerWeight[candidate]; ok {
			return candidate
		}

		// Try joining with next part for multi-segment quants like "Q4_K" + "M"
		// which shouldn't happen in practice, but handle edge cases.
		if i+1 < len(parts) {
			candidate = strings.ToUpper(parts[i] + "_" + parts[i+1])
			if _, ok := QuantBitsPerWeight[candidate]; ok {
				return candidate
			}
		}
	}

	return ""
}

// IsGGUF returns true if the filename ends with .gguf (case-insensitive).
func IsGGUF(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".gguf")
}

// FilterGGUF filters a list of FileInfo to only include GGUF files.
func FilterGGUF(files []FileInfo) []FileInfo {
	var result []FileInfo
	for _, f := range files {
		if IsGGUF(f.Filename) {
			result = append(result, f)
		}
	}
	return result
}

// FilterByQuant filters files to only include those matching the given quantization.
func FilterByQuant(files []FileInfo, quant string) []FileInfo {
	quant = strings.ToUpper(quant)
	var result []FileInfo
	for _, f := range files {
		if strings.EqualFold(f.Quantization, quant) {
			result = append(result, f)
		}
	}
	return result
}

// QuantGroup groups split GGUF shards by quantization level.
type QuantGroup struct {
	Quantization  string
	BitsPerWeight float64
	Files         []FileInfo // individual shards, sorted by filename
	TotalSize     int64
	ShardCount    int
}

// GroupByQuant groups GGUF files by quantization type. Split shards
// (e.g. model-Q4_K_M-00001-of-00002.gguf) are collapsed into a single
// group. Files with no recognized quantization are placed in a group
// with Quantization="".
func GroupByQuant(files []FileInfo) []QuantGroup {
	order := make([]string, 0)
	groups := make(map[string]*QuantGroup)

	for _, f := range files {
		q := f.Quantization
		g, ok := groups[q]
		if !ok {
			g = &QuantGroup{
				Quantization:  q,
				BitsPerWeight: f.BitsPerWeight,
			}
			groups[q] = g
			order = append(order, q)
		}
		g.Files = append(g.Files, f)
		g.TotalSize += f.Size
		g.ShardCount++
	}

	// Sort each group's files by name so shard 00001 comes first.
	for _, g := range groups {
		sort.Slice(g.Files, func(i, j int) bool {
			return g.Files[i].Filename < g.Files[j].Filename
		})
	}

	// Build result sorted by bits-per-weight descending.
	result := make([]QuantGroup, 0, len(groups))
	for _, q := range order {
		result = append(result, *groups[q])
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].BitsPerWeight > result[j].BitsPerWeight
	})

	return result
}

// SortBySize sorts files by size ascending.
func SortBySize(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size < files[j].Size
	})
}

// SortByQuality sorts files by bits-per-weight descending (highest quality first).
func SortByQuality(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].BitsPerWeight > files[j].BitsPerWeight
	})
}
