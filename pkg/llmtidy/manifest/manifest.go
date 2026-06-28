// Package manifest parses, validates, and persists the llm-tidy desired-state manifest.
package manifest

import (
	"errors"
	"fmt"
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

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest parse error: %w", err)
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write manifest: %w", err)
	}
	return nil
}
