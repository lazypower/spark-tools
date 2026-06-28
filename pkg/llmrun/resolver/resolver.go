// Package resolver is a compatibility wrapper over internal/modelref. The model-
// reference resolver (local paths, aliases, hfetch registry refs, hf:// URIs) and
// the alias store moved to internal/modelref during the /internal extraction; this
// thin alias keeps existing importers (cmd/llm-run, pkg/llmrun, pkg/llmbench)
// compiling unchanged until they migrate. Type aliases carry the Resolver and
// ResolveSource methods over; the constructor and alias funcs delegate.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/modelref.
package resolver

import (
	mref "github.com/lazypower/spark-tools/internal/modelref"
)

// Type aliases — carry methods (ResolveSource.String, Resolver.ResolveModel) over
// and keep ResolvedModel values flowing across the boundary as the same type.
type (
	ResolveSource = mref.ResolveSource
	ResolvedModel = mref.ResolvedModel
	Resolver      = mref.Resolver
)

// Resolve source enum.
const (
	ResolveSourceLocalPath = mref.ResolveSourceLocalPath
	ResolveSourceAlias     = mref.ResolveSourceAlias
	ResolveSourceRegistry  = mref.ResolveSourceRegistry
	ResolveSourceHFPull    = mref.ResolveSourceHFPull
)

// NewResolver builds a resolver over the given config + registry data dirs.
func NewResolver(configDir, registryDataDir string) *Resolver {
	return mref.NewResolver(configDir, registryDataDir)
}

// LoadAliases reads the alias map from configDir.
func LoadAliases(configDir string) (map[string]string, error) { return mref.LoadAliases(configDir) }

// SaveAliases writes the alias map to configDir.
func SaveAliases(configDir string, aliases map[string]string) error {
	return mref.SaveAliases(configDir, aliases)
}

// SetAlias adds or updates an alias.
func SetAlias(configDir, name, ref string) error { return mref.SetAlias(configDir, name, ref) }

// RemoveAlias deletes an alias.
func RemoveAlias(configDir, name string) error { return mref.RemoveAlias(configDir, name) }

// ListAliases returns the alias map.
func ListAliases(configDir string) (map[string]string, error) { return mref.ListAliases(configDir) }
