// Package gguf is a compatibility wrapper over internal/gguf. The GGUF parsing,
// quant-from-filename, fit estimation, and shard merge authority moved to
// internal/gguf during the /internal extraction; this thin alias keeps existing
// importers (cmd/hfetch, cmd/llm-run, internal/ui, pkg/hfetch, pkg/llmrun,
// pkg/llmtidy) compiling unchanged until they migrate.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/gguf.
package gguf

import (
	"io"

	igguf "github.com/lazypower/spark-tools/internal/gguf"
)

// Types (aliases).
type (
	FileInfo     = igguf.FileInfo
	QuantGroup   = igguf.QuantGroup
	FitStatus    = igguf.FitStatus
	FitResult    = igguf.FitResult
	GGUFMetadata = igguf.GGUFMetadata
	KV           = igguf.KV
	TensorInfo   = igguf.TensorInfo
	TypedArray   = igguf.TypedArray
	ShardHeader  = igguf.ShardHeader
)

// FitStatus values.
const (
	FitYes     = igguf.FitYes
	FitTight   = igguf.FitTight
	FitNo      = igguf.FitNo
	FitUnknown = igguf.FitUnknown
)

// Lookup tables (shared — same map values as the authority).
var (
	FileTypeNames      = igguf.FileTypeNames
	QuantBitsPerWeight = igguf.QuantBitsPerWeight
)

// Functions (delegating).
func ParseQuantFromFilename(filename string) string { return igguf.ParseQuantFromFilename(filename) }
func IsGGUF(filename string) bool                   { return igguf.IsGGUF(filename) }
func FilterGGUF(files []FileInfo) []FileInfo        { return igguf.FilterGGUF(files) }
func FilterByQuant(files []FileInfo, quant string) []FileInfo {
	return igguf.FilterByQuant(files, quant)
}
func GroupByQuant(files []FileInfo) []QuantGroup { return igguf.GroupByQuant(files) }
func SortBySize(files []FileInfo)                { igguf.SortBySize(files) }
func SortByQuality(files []FileInfo)             { igguf.SortByQuality(files) }
func EstimateFit(fileSizeBytes int64, meta *GGUFMetadata, availableGB float64) FitResult {
	return igguf.EstimateFit(fileSizeBytes, meta, availableGB)
}
func MergeShards(shardPaths []string, outputPath string) error {
	return igguf.MergeShards(shardPaths, outputPath)
}
func Parse(r io.Reader) (*GGUFMetadata, error)          { return igguf.Parse(r) }
func ParseShard(rs io.ReadSeeker) (*ShardHeader, error) { return igguf.ParseShard(rs) }
func QuantQualityLabel(quant string) string             { return igguf.QuantQualityLabel(quant) }
