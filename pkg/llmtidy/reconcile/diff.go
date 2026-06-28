// Package reconcile diffs a manifest against inventory and applies prune/sync plans.
package reconcile

import (
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
	"github.com/lazypower/spark-tools/pkg/llmtidy/manifest"
)

// ModelSpec is a manifest entry tagged with its backend.
type ModelSpec struct {
	Backend inventory.ModelBackend
	Ollama  *manifest.OllamaModelSpec
	GGUF    *manifest.GGUFModelSpec
	VLLM    *manifest.VLLMModelSpec
}

// Name returns a human-readable identifier for the spec.
func (s ModelSpec) Name() string {
	switch s.Backend {
	case inventory.BackendOllama:
		if s.Ollama == nil {
			return ""
		}
		return s.Ollama.NormalizedName()
	case inventory.BackendGGUF:
		if s.GGUF == nil {
			return ""
		}
		if s.GGUF.Quant == "" {
			return s.GGUF.Repo
		}
		return s.GGUF.Repo + " " + s.GGUF.Quant
	case inventory.BackendVLLM:
		if s.VLLM == nil {
			return ""
		}
		return s.VLLM.Repo
	}
	return ""
}

// DiffResult categorizes the manifest-vs-inventory comparison per spec §10.1.
type DiffResult struct {
	Blessed   []inventory.InstalledModel
	Untracked []inventory.InstalledModel
	Missing   []ModelSpec
}

// Diff compares the manifest against an inventory snapshot.
//
// Matching semantics per spec §10.2:
//
//   - Ollama: exact name match after :latest normalization.
//   - GGUF: case-insensitive repo. When quant is specified in the manifest,
//     it must also match; when omitted, any quant from the repo matches.
func Diff(m *manifest.Manifest, installed []inventory.InstalledModel) DiffResult {
	if m == nil {
		return DiffResult{Untracked: installed}
	}

	ollamaSeen := make([]bool, len(m.Ollama))
	ggufSeen := make([]bool, len(m.GGUF))
	vllmSeen := make([]bool, len(m.VLLM))

	var blessed, untracked []inventory.InstalledModel

	for _, im := range installed {
		matched := false
		switch im.Backend {
		case inventory.BackendOllama:
			for i, spec := range m.Ollama {
				if matchesOllama(spec, im) {
					ollamaSeen[i] = true
					matched = true
					break
				}
			}
		case inventory.BackendGGUF:
			for i, spec := range m.GGUF {
				if matchesGGUF(spec, im) {
					ggufSeen[i] = true
					matched = true
					break
				}
			}
		case inventory.BackendVLLM:
			for i, spec := range m.VLLM {
				if matchesVLLM(spec, im) {
					vllmSeen[i] = true
					matched = true
					break
				}
			}
		}
		if matched {
			blessed = append(blessed, im)
		} else {
			untracked = append(untracked, im)
		}
	}

	var missing []ModelSpec
	for i, spec := range m.Ollama {
		if !ollamaSeen[i] {
			s := spec
			missing = append(missing, ModelSpec{Backend: inventory.BackendOllama, Ollama: &s})
		}
	}
	for i, spec := range m.GGUF {
		if !ggufSeen[i] {
			s := spec
			missing = append(missing, ModelSpec{Backend: inventory.BackendGGUF, GGUF: &s})
		}
	}
	for i, spec := range m.VLLM {
		if !vllmSeen[i] {
			s := spec
			missing = append(missing, ModelSpec{Backend: inventory.BackendVLLM, VLLM: &s})
		}
	}

	return DiffResult{
		Blessed:   blessed,
		Untracked: untracked,
		Missing:   missing,
	}
}

// matchesOllama implements spec §10.2 Ollama matching.
func matchesOllama(spec manifest.OllamaModelSpec, im inventory.InstalledModel) bool {
	if im.Backend != inventory.BackendOllama {
		return false
	}
	return spec.NormalizedName() == manifest.NormalizeOllamaName(im.OllamaName)
}

// matchesGGUF implements spec §10.2 GGUF matching.
func matchesGGUF(spec manifest.GGUFModelSpec, im inventory.InstalledModel) bool {
	if im.Backend != inventory.BackendGGUF {
		return false
	}
	if !strings.EqualFold(spec.Repo, im.Repo) {
		return false
	}
	if spec.Quant == "" {
		return true
	}
	return spec.Quant == im.Quant
}

// matchesVLLM matches a vLLM spec by case-insensitive repo id.
func matchesVLLM(spec manifest.VLLMModelSpec, im inventory.InstalledModel) bool {
	return im.Backend == inventory.BackendVLLM && strings.EqualFold(spec.Repo, im.Repo)
}
