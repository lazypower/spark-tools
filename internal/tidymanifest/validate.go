package tidymanifest

import (
	"fmt"
	"strings"
)

// Validate checks structural and semantic invariants of the manifest:
//
//   - Version must equal SchemaVersion.
//   - Every Ollama entry must have a non-empty name.
//   - Every GGUF entry must have a non-empty repo.
//   - No duplicate Ollama names (after :latest normalization).
//   - No duplicate (repo, quant) pairs in GGUF entries.
func Validate(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if m.Version != SchemaVersion {
		return fmt.Errorf("unsupported manifest version %d (this build supports version %d)", m.Version, SchemaVersion)
	}

	seenOllama := make(map[string]int)
	for i, spec := range m.Ollama {
		if strings.TrimSpace(spec.Name) == "" {
			return fmt.Errorf("ollama[%d]: name is required", i)
		}
		key := spec.NormalizedName()
		if prev, ok := seenOllama[key]; ok {
			return fmt.Errorf("ollama[%d]: duplicate name %q (also at ollama[%d])", i, key, prev)
		}
		seenOllama[key] = i
	}

	seenGGUF := make(map[string]int)
	for i, spec := range m.GGUF {
		if strings.TrimSpace(spec.Repo) == "" {
			return fmt.Errorf("gguf[%d]: repo is required", i)
		}
		key := strings.ToLower(spec.Repo) + "|" + spec.Quant
		if prev, ok := seenGGUF[key]; ok {
			return fmt.Errorf("gguf[%d]: duplicate spec %s/%s (also at gguf[%d])", i, spec.Repo, spec.Quant, prev)
		}
		seenGGUF[key] = i
	}

	return nil
}
