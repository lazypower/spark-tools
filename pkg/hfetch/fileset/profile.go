// Package fileset selects and verifies curated file sets ("profiles") for a
// HuggingFace model repo. A profile decides which files a "serve-ready" pull
// must fetch (P0.1); the completeness gate (completeness.go) verifies the
// downloaded set is whole (P0.2). See specs/01-hfetch.md §14.
//
// The curated vLLM include/exclude set is the oracle pinned at
// gitea.wabash.place/lab/vllm-config:docs/vllm-fileset.md — distilled from
// hand-pulling NVFP4/GPTQ/compressed-tensors/vision models. Each rule below
// maps to a line in that doc.
package fileset

import (
	"path"
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// Profile names a fileset selector.
type Profile string

const (
	// ProfileGGUF is the GGUF-picker behavior (one quant file). Selection for
	// this profile lives in pkg/gguf; it is named here for the --profile flag.
	ProfileGGUF Profile = "gguf"

	// ProfileVLLM fetches the complete serve-ready safetensors fileset.
	ProfileVLLM Profile = "vllm"
)

// vllmInclude is the curated include set. Globs match against a file's base
// name (these files live at the repo root). A file is selected iff it matches
// an include glob AND no exclude glob.
var vllmInclude = []string{
	// Weights — glob ALL .safetensors (some repos ship weights not in the
	// index, e.g. an MTP head) plus the shard map.
	"*.safetensors",
	"*.safetensors.index.json",
	// Config.
	"config.json",
	"generation_config.json",
	"configuration.json",
	// Quant metadata (ModelOpt NVFP4/FP8, GPTQ). Pulled iff present in tree.
	"hf_quant_config.json",
	"quantize_config.json",
	// Tokenizer — every variant present; a model needs at least one.
	"tokenizer.json",
	"tokenizer_config.json",
	"tekken.json",
	"vocab.json",
	"merges.txt",
	"special_tokens_map.json",
	"added_tokens.json",
	"*.model", // SentencePiece
	"*.spm",
	// Chat template (may instead be embedded in tokenizer_config.json).
	"*.jinja",
	// Custom code for trust-remote-code: glob ALL .py — modeling /
	// configuration / __init__ AND reasoning-parser plugins (which auto_map
	// never names; see completeness.go).
	"*.py",
	// Multimodal processors (vision/omni models).
	"preprocessor_config.json",
	"processor_config.json",
	"video_preprocessor_config.json",
	// Recommended system prompts (optional; junk .txt filtered by exclude).
	"*.txt",
}

// vllmExclude is junk we never pull for serving. Applied after include, so it
// also trims .txt/.json that an include glob would otherwise catch
// (.quant_summary.txt, evaluation.json, preds.json).
var vllmExclude = []string{
	"*.md", // README and all doc cards (bias/explainability/privacy/safety)
	".gitattributes",
	"recipe.yaml",
	"*.png",
	"evaluation.json",
	"preds.json",
	".quant_summary.txt",
}

// SelectVLLM returns the subset of repo files the vLLM serve-ready profile must
// fetch. Directory entries are ignored (ListFiles already drops them, but we
// guard regardless).
func SelectVLLM(files []api.ModelFile) []api.ModelFile {
	var out []api.ModelFile
	for _, f := range files {
		if f.Type == "directory" {
			continue
		}
		base := path.Base(f.Filename)
		if matchAny(base, vllmExclude) {
			continue
		}
		if matchAny(base, vllmInclude) {
			out = append(out, f)
		}
	}
	return out
}

// matchAny reports whether name matches any of the glob patterns. Patterns use
// path.Match semantics (`*` does not cross `/`, but name is already a base).
// A leading-dot literal like ".gitattributes" matches exactly.
func matchAny(name string, patterns []string) bool {
	for _, p := range patterns {
		// Literal (no glob metacharacters) → exact compare, so dotfiles and
		// fixed names match without path.Match's quirks.
		if !strings.ContainsAny(p, "*?[") {
			if name == p {
				return true
			}
			continue
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
