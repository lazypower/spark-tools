// Package tidymanifest parses, validates, and persists the llm-tidy
// desired-state manifest. Extracted from pkg/llmtidy/manifest during the
// /internal extraction; pkg/llmtidy/manifest remains as a compat wrapper.
package tidymanifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the manifest schema version this package writes and accepts.
const SchemaVersion = 1

// Manifest is the desired model state for a machine.
type Manifest struct {
	Version int               `yaml:"version"`
	Ollama  []OllamaModelSpec `yaml:"ollama,omitempty"`
	GGUF    []GGUFModelSpec   `yaml:"gguf,omitempty"`
	VLLM    []VLLMModelSpec   `yaml:"vllm,omitempty"`
}

// OllamaModelSpec declares a model that should exist in Ollama.
type OllamaModelSpec struct {
	Name string `yaml:"name"`
}

// GGUFModelSpec declares a model that should exist in the hfetch registry.
type GGUFModelSpec struct {
	Repo  string `yaml:"repo"`
	Quant string `yaml:"quant,omitempty"`
}

// VLLMModelSpec declares an HF-format (safetensors) model that should exist in
// the hfetch registry — matched by repo id.
type VLLMModelSpec struct {
	Repo string `yaml:"repo"`
}

// NormalizedName returns the Ollama spec name with ":latest" appended when no
// tag is present, matching Ollama's own default-tag convention.
func (s OllamaModelSpec) NormalizedName() string {
	return NormalizeOllamaName(s.Name)
}

// NormalizeOllamaName appends ":latest" when no tag is present.
func NormalizeOllamaName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, ":") {
		return name
	}
	return name + ":latest"
}

// ErrNotFound is returned by Load when the manifest file does not exist.
var ErrNotFound = errors.New("no manifest found")

// Load reads and parses a manifest from the given path. Returns ErrNotFound
// if the file does not exist so callers can offer the "run llm-tidy init"
// remediation from spec §9.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// The manifest is a deletion-safety boundary: a misspelled section (e.g.
	// `guff:` instead of `gguf:`) must not silently parse to an empty list and
	// unbless every model in it. Decode strictly so unknown keys are rejected.
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty file is a well-formed empty manifest, not a parse error.
			return &m, nil
		}
		return nil, fmt.Errorf("manifest parse error: %w", err)
	}
	// Reject multi-document manifests: a stray `---` separator would otherwise
	// silently drop every entry after the first document, under-blessing models
	// that prune would then delete. The manifest must be a single document.
	if err := dec.Decode(new(Manifest)); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("manifest parse error: %w", err)
		}
		return nil, fmt.Errorf("manifest parse error: multiple YAML documents found; the manifest must be a single document")
	}
	return &m, nil
}

// Save writes the manifest as YAML to the given path, creating parent
// directories as needed.
func Save(m *Manifest, path string) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create manifest directory: %w", err)
	}

	out := *m
	if out.Version == 0 {
		out.Version = SchemaVersion
	}
	data, err := yaml.Marshal(&out)
	if err != nil {
		return err
	}

	// Write atomically: a truncate-then-write that is interrupted (crash, full
	// disk, concurrent writer) would corrupt the manifest and, because Load
	// hard-errors on a bad parse, brick every llm-tidy command until it is
	// hand-repaired. Stage in a temp file in the same directory, fsync, then
	// rename into place so a reader ever sees only the old or the new file.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-manifest-*")
	if err != nil {
		return fmt.Errorf("cannot create temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	// CreateTemp makes the file 0600; the manifest is not secret and callers
	// expect the prior 0644, so restore it before the rename.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot set manifest permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot sync manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot write manifest: %w", err)
	}
	return nil
}
