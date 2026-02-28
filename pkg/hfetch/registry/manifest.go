// Package registry manages the local model registry, tracking
// downloaded models in a JSON manifest.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manifest is the on-disk registry of all downloaded models.
type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	Models        []LocalModel `json:"models"`
}

// LocalModel represents a downloaded model in the local registry.
type LocalModel struct {
	ID           string      `json:"id"`
	Author       string      `json:"author,omitempty"`
	DownloadedAt time.Time   `json:"downloaded_at"`
	Files        []LocalFile `json:"files"`
}

// LocalFile is a model file that exists on disk.
type LocalFile struct {
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256,omitempty"`
	Quantization string    `json:"quantization,omitempty"`
	LocalPath    string    `json:"local_path"`
	Complete     bool      `json:"complete"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

// Registry provides access to locally downloaded models.
type Registry struct {
	mu       sync.RWMutex
	dataDir  string
	manifest *Manifest
}

// New creates a Registry backed by the given data directory.
func New(dataDir string) *Registry {
	return &Registry{dataDir: dataDir}
}

// Load reads the manifest from disk. If no manifest exists, an empty one is created.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := r.manifestPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		r.manifest = &Manifest{SchemaVersion: 1}
		return nil
	}
	if err != nil {
		return err
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	r.manifest = &m
	return nil
}

// Save writes the manifest to disk.
func (r *Registry) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := os.MkdirAll(r.dataDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(r.manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.manifestPath(), data, 0644)
}

// List returns all registered local models.
func (r *Registry) List() []LocalModel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.manifest == nil {
		return nil
	}
	return r.manifest.Models
}

// Get returns a specific model by ID, or nil if not found.
func (r *Registry) Get(modelID string) *LocalModel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := range r.manifest.Models {
		if r.manifest.Models[i].ID == modelID {
			return &r.manifest.Models[i]
		}
	}
	return nil
}

// Path returns the local path for a specific file in a model.
// If filename is empty, returns the path to the first complete file.
func (r *Registry) Path(modelID, filename string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, m := range r.manifest.Models {
		if m.ID != modelID {
			continue
		}
		for _, f := range m.Files {
			if !f.Complete {
				continue
			}
			if filename == "" || f.Filename == filename {
				return f.LocalPath
			}
		}
	}
	return ""
}

// AddFile registers a downloaded file for a model.
func (r *Registry) AddFile(modelID string, file LocalFile) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.manifest.Models {
		if r.manifest.Models[i].ID == modelID {
			// Update existing file or append.
			for j := range r.manifest.Models[i].Files {
				if r.manifest.Models[i].Files[j].Filename == file.Filename {
					r.manifest.Models[i].Files[j] = file
					return
				}
			}
			r.manifest.Models[i].Files = append(r.manifest.Models[i].Files, file)
			return
		}
	}

	// New model entry.
	r.manifest.Models = append(r.manifest.Models, LocalModel{
		ID:           modelID,
		Author:       extractAuthor(modelID),
		DownloadedAt: time.Now(),
		Files:        []LocalFile{file},
	})
}

// Remove removes a model (or specific files) from the registry and disk.
func (r *Registry) Remove(modelID string, filenames ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.manifest.Models {
		if r.manifest.Models[i].ID != modelID {
			continue
		}

		if len(filenames) == 0 {
			// Remove entire model directory.
			dir := r.ModelDir(modelID)
			if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
				return err
			}
			r.manifest.Models = append(r.manifest.Models[:i], r.manifest.Models[i+1:]...)
			return nil
		}

		// Remove specific files.
		removeSet := make(map[string]bool)
		for _, f := range filenames {
			removeSet[f] = true
		}

		var remaining []LocalFile
		for _, f := range r.manifest.Models[i].Files {
			if removeSet[f.Filename] {
				os.Remove(f.LocalPath)
			} else {
				remaining = append(remaining, f)
			}
		}
		r.manifest.Models[i].Files = remaining

		// Remove model entry if no files remain.
		if len(remaining) == 0 {
			dir := r.ModelDir(modelID)
			os.RemoveAll(dir)
			r.manifest.Models = append(r.manifest.Models[:i], r.manifest.Models[i+1:]...)
		}
		return nil
	}
	return nil
}

// ModelDir returns the directory path for a model's files.
// Uses the org--model convention (e.g., "TheBloke--Llama-2-7B-GGUF").
func (r *Registry) ModelDir(modelID string) string {
	safeName := strings.ReplaceAll(modelID, "/", "--")
	return filepath.Join(r.dataDir, "models", safeName)
}

func (r *Registry) manifestPath() string {
	return filepath.Join(r.dataDir, "manifest.json")
}

func extractAuthor(modelID string) string {
	if i := strings.Index(modelID, "/"); i >= 0 {
		return modelID[:i]
	}
	return ""
}
