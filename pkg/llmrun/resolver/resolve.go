// Package resolver parses model references and resolves them to
// local file paths via aliases, hfetch registry, or direct paths.
package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// ResolveSource describes how a model reference was resolved.
type ResolveSource int

const (
	ResolveSourceLocalPath ResolveSource = iota // Direct file path
	ResolveSourceAlias                          // Resolved via alias
	ResolveSourceRegistry                       // Resolved via hfetch registry
	ResolveSourceHFPull                         // Downloaded from HuggingFace
)

// String returns a human-readable label for the resolve source.
func (s ResolveSource) String() string {
	switch s {
	case ResolveSourceLocalPath:
		return "local_path"
	case ResolveSourceAlias:
		return "alias"
	case ResolveSourceRegistry:
		return "registry"
	case ResolveSourceHFPull:
		return "hf_pull"
	default:
		return "unknown"
	}
}

// ResolvedModel captures how a model reference was resolved.
type ResolvedModel struct {
	Path          string             // Absolute path to the .gguf file on disk
	Source        ResolveSource      // How it was resolved
	RequestedRef  string             // Raw input as the user typed it
	NormalizedRef string             // Canonical form (derived from RequestedRef)
	Quant         string             // Quantization type (e.g. "Q4_K_M")
	RegistryID    string             // hfetch model ID, if resolved via registry
	GGUFMeta      *gguf.GGUFMetadata // Parsed GGUF header metadata (nil if unavailable)
	WasPulled     bool               // True if hfetch downloaded it during resolution
}

// Resolver resolves model references to local file paths.
type Resolver struct {
	configDir      string
	registryDataDir string
}

// NewResolver creates a Resolver. configDir is the llm-run config
// directory (for aliases.json). registryDataDir is the hfetch data
// directory (for the model registry/manifest).
func NewResolver(configDir string, registryDataDir string) *Resolver {
	return &Resolver{
		configDir:       configDir,
		registryDataDir: registryDataDir,
	}
}

// ResolveModel resolves a model reference string to a ResolvedModel.
//
// The detection heuristic is evaluated in order (first match wins):
//  1. hf:// prefix        -> HuggingFace URI (auto-pull if missing)
//  2. Starts with /, ./, ~/ or ends with .gguf -> local file path
//  3. No / in the string  -> alias lookup
//  4. Contains /          -> hfetch registry ref (org/model:quant)
func (r *Resolver) ResolveModel(ctx context.Context, ref string) (*ResolvedModel, error) {
	if ref == "" {
		return nil, fmt.Errorf("model reference must not be empty")
	}

	// 1. HuggingFace URI (hf:// prefix)
	if strings.HasPrefix(ref, "hf://") {
		return r.resolveHFURI(ctx, ref)
	}

	// 2. Local file path
	if isLocalPath(ref) {
		return r.resolveLocalPath(ref)
	}

	// 3. Alias (no / in string)
	if !strings.Contains(ref, "/") {
		return r.resolveAlias(ctx, ref)
	}

	// 4. Registry ref (contains /)
	return r.resolveRegistryRef(ctx, ref)
}

// isLocalPath returns true when the reference looks like a filesystem path.
func isLocalPath(ref string) bool {
	return strings.HasPrefix(ref, "/") ||
		strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "~/") ||
		strings.HasSuffix(ref, ".gguf")
}

// resolveHFURI handles refs with the hf:// prefix.
func (r *Resolver) resolveHFURI(_ context.Context, ref string) (*ResolvedModel, error) {
	// Strip the hf:// prefix to get org/model (and optional :quant).
	stripped := strings.TrimPrefix(ref, "hf://")
	modelID, quant := parseRegistryRef(stripped)

	// Check if already in the local registry.
	reg := registry.New(r.registryDataDir)
	if err := reg.Load(); err != nil {
		return nil, fmt.Errorf("loading hfetch registry: %w", err)
	}

	path := r.findInRegistry(reg, modelID, quant)
	if path != "" {
		return &ResolvedModel{
			Path:          path,
			Source:        ResolveSourceHFPull,
			RequestedRef:  ref,
			NormalizedRef: modelID,
			Quant:         quant,
			RegistryID:    modelID,
			GGUFMeta:      tryParseGGUF(path),
			WasPulled:     false,
		}, nil
	}

	// Not found locally — flag for pull.
	return &ResolvedModel{
		Path:          "",
		Source:        ResolveSourceHFPull,
		RequestedRef:  ref,
		NormalizedRef: modelID,
		Quant:         quant,
		RegistryID:    modelID,
		WasPulled:     false,
	}, nil
}

// resolveLocalPath resolves a direct file-system path.
func (r *Resolver) resolveLocalPath(ref string) (*ResolvedModel, error) {
	path := ref
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("expanding ~: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", ref, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("model file not found: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("model path %q is a directory, not a file", absPath)
	}

	result := &ResolvedModel{
		Path:          absPath,
		Source:        ResolveSourceLocalPath,
		RequestedRef:  ref,
		NormalizedRef: absPath,
	}
	result.GGUFMeta = tryParseGGUF(absPath)
	return result, nil
}

// resolveAlias resolves a short alias to a model reference, then
// recursively resolves the target.
func (r *Resolver) resolveAlias(ctx context.Context, ref string) (*ResolvedModel, error) {
	aliases, err := LoadAliases(r.configDir)
	if err != nil {
		return nil, fmt.Errorf("loading aliases: %w", err)
	}

	target, ok := aliases[ref]
	if !ok {
		return nil, fmt.Errorf("model alias %q not found (use 'llm-run alias set %s <ref>' to create)", ref, ref)
	}

	resolved, err := r.ResolveModel(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("resolving alias %q -> %q: %w", ref, target, err)
	}
	resolved.Source = ResolveSourceAlias
	resolved.RequestedRef = ref
	return resolved, nil
}

// resolveRegistryRef resolves an org/model:quant reference via the
// hfetch registry.
func (r *Resolver) resolveRegistryRef(_ context.Context, ref string) (*ResolvedModel, error) {
	modelID, quant := parseRegistryRef(ref)

	reg := registry.New(r.registryDataDir)
	if err := reg.Load(); err != nil {
		return nil, fmt.Errorf("loading hfetch registry: %w", err)
	}

	path := r.findInRegistry(reg, modelID, quant)
	if path == "" {
		return nil, fmt.Errorf("model %q not found in local registry (use 'hfetch pull %s' to download)", ref, ref)
	}

	return &ResolvedModel{
		Path:          path,
		Source:        ResolveSourceRegistry,
		RequestedRef:  ref,
		NormalizedRef: modelID,
		Quant:         quant,
		RegistryID:    modelID,
		GGUFMeta:      tryParseGGUF(path),
	}, nil
}

// parseRegistryRef splits "org/model:Q4_K_M" into modelID and quant.
// If no colon is present, quant is empty.
func parseRegistryRef(ref string) (modelID, quant string) {
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

// findInRegistry looks up a model in the hfetch registry and returns
// the local file path. When quant is specified, it matches on the
// quantization field of the file entry. When quant is empty, the
// first complete file is returned.
//
// For split models (multiple shards with the same quant), the path to
// the first shard is returned (sorted lexically, so -00001-of-N wins).
// llama.cpp locates sibling shards automatically.
func (r *Resolver) findInRegistry(reg *registry.Registry, modelID, quant string) string {
	model := reg.Get(modelID)
	if model == nil {
		return ""
	}

	var bestPath string
	for _, f := range model.Files {
		if !f.Complete {
			continue
		}
		if quant == "" || strings.EqualFold(f.Quantization, quant) {
			if bestPath == "" || f.LocalPath < bestPath {
				bestPath = f.LocalPath
			}
		}
	}
	return bestPath
}

// tryParseGGUF attempts to parse GGUF metadata from a file.
// Returns nil on any error (best-effort).
func tryParseGGUF(path string) *gguf.GGUFMetadata {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	meta, err := gguf.Parse(f)
	if err != nil {
		return nil
	}
	return meta
}
