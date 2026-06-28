// Package registry is a compatibility wrapper over internal/modelstore. The
// local model registry authority moved to internal/modelstore during the
// /internal extraction; this thin alias keeps existing importers (cmd/hfetch,
// pkg/hfetch, pkg/llmrun/resolver, pkg/llmtidy*, pkg/seam) compiling unchanged
// until they migrate. Type aliases carry every method over for free.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/modelstore.
package registry

import "github.com/lazypower/spark-tools/internal/modelstore"

// Registry / manifest / storage types (aliases — methods ride along).
type (
	Manifest      = modelstore.Manifest
	LocalModel    = modelstore.LocalModel
	LocalFile     = modelstore.LocalFile
	Registry      = modelstore.Registry
	StorageLayout = modelstore.StorageLayout
)

// New constructs a Registry rooted at dataDir. Delegates to internal/modelstore.
func New(dataDir string) *Registry { return modelstore.New(dataDir) }

// NewStorageLayout constructs a StorageLayout rooted at dataDir.
func NewStorageLayout(dataDir string) *StorageLayout { return modelstore.NewStorageLayout(dataDir) }
