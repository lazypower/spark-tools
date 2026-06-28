// Package inventory enumerates installed models across the Ollama and GGUF backends.
package inventory

import (
	"fmt"
	"time"
)

// ModelBackend identifies which storage system holds a model.
type ModelBackend int

const (
	// BackendUnknown is the zero value and should not appear in inventory rows.
	BackendUnknown ModelBackend = iota
	BackendOllama
	BackendGGUF
)

// String returns the lower-case backend name used in CLI flags and output.
func (b ModelBackend) String() string {
	switch b {
	case BackendOllama:
		return "ollama"
	case BackendGGUF:
		return "gguf"
	default:
		return "unknown"
	}
}

// ParseBackend converts a CLI flag value to a ModelBackend.
func ParseBackend(s string) (ModelBackend, error) {
	switch s {
	case "ollama":
		return BackendOllama, nil
	case "gguf":
		return BackendGGUF, nil
	default:
		return BackendUnknown, fmt.Errorf("unknown backend %q (want \"ollama\" or \"gguf\")", s)
	}
}

// InstalledModel is a model present on disk, regardless of backend.
type InstalledModel struct {
	// Name is the display name. For Ollama, the model:tag; for GGUF, the
	// repo (with quant suffix if known).
	Name string

	// Backend identifies which storage system holds this model.
	Backend ModelBackend

	// Size in bytes.
	Size int64

	// Modified is the last-modified timestamp.
	Modified time.Time

	// OllamaName is the canonical Ollama model:tag. Empty for GGUF.
	OllamaName string

	// Repo is the hfetch repo ID. Empty for Ollama.
	Repo string

	// Quant is the parsed quantization (e.g. "Q4_K_M"). Empty for Ollama or
	// when unknown.
	Quant string

	// Filename is the on-disk filename within the hfetch registry. Empty
	// for Ollama.
	Filename string

	// Path is the on-disk host path of the model file/dir. Set for path-based
	// backends (GGUF); EMPTY for Ollama, which is deleted via its own API by name
	// and is governed by Ollama's runtime, not the llm-serve eviction interlock.
	Path string
}
