package registry

import (
	"os"
	"path/filepath"
	"strings"
)

// StorageLayout handles disk layout and path resolution for model files.
type StorageLayout struct {
	DataDir string
}

// NewStorageLayout creates a StorageLayout rooted at the given data directory.
func NewStorageLayout(dataDir string) *StorageLayout {
	return &StorageLayout{DataDir: dataDir}
}

// ModelDir returns the directory path for a model's files.
// Uses the org--model convention (e.g., "TheBloke--Llama-2-7B-GGUF").
func (s *StorageLayout) ModelDir(modelID string) string {
	safeName := strings.ReplaceAll(modelID, "/", "--")
	return filepath.Join(s.DataDir, "models", safeName)
}

// FilePath returns the full path where a specific file should be stored.
func (s *StorageLayout) FilePath(modelID, filename string) string {
	return filepath.Join(s.ModelDir(modelID), filename)
}

// PartialPath returns the path for an in-progress download.
func (s *StorageLayout) PartialPath(modelID, filename string) string {
	return s.FilePath(modelID, filename) + ".partial"
}

// StatePath returns the path for a download state sidecar.
func (s *StorageLayout) StatePath(modelID, filename string) string {
	return s.FilePath(modelID, filename) + ".state"
}

// EnsureModelDir creates the model directory if it doesn't exist.
func (s *StorageLayout) EnsureModelDir(modelID string) error {
	return os.MkdirAll(s.ModelDir(modelID), 0700)
}

// ManifestPath returns the path to the manifest file.
func (s *StorageLayout) ManifestPath() string {
	return filepath.Join(s.DataDir, "manifest.json")
}
