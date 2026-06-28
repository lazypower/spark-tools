// Package serveartifact reads the serving-relevant facts off a verified model
// directory and is the boundary to hfetch's completeness gate. It is the read
// side of design §5: model resolution goes THROUGH hfetch — verified,
// immutable-revision artifacts only — and the artifact/load gate is delegated to
// hfetch (pkg/hfetch/fileset), never reimplemented here (one authority). Verify
// refuses to surface facts for an artifact that has not passed that gate, so
// llm-serve can never emit a launch for an unverified model.
package serveartifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lazypower/spark-tools/internal/hub"
	"github.com/lazypower/spark-tools/internal/fileset"
	"github.com/lazypower/spark-tools/internal/modelmeta"
	"github.com/lazypower/spark-tools/internal/serving"
)

// Verify runs hfetch's completeness gate against the artifact directory and, only
// when it passes, detects and returns the serving facts. repoFiles is the repo's
// file tree (the gate's authority, from hub.ListFiles); dir is the local
// directory the model was pulled into. An incomplete artifact is rejected with
// the named hard-fails — llm-serve does not emit a launch for a model hfetch
// would not certify serve-ready.
func Verify(repoFiles []hub.ModelFile, dir string) (serving.ArtifactFacts, error) {
	rep, err := fileset.Verify(repoFiles, dir)
	if err != nil {
		return serving.ArtifactFacts{}, fmt.Errorf("completeness gate could not run: %w", err)
	}
	if !rep.Complete() {
		var names []string
		for _, iss := range rep.HardFail {
			names = append(names, iss.String())
		}
		return serving.ArtifactFacts{}, fmt.Errorf("artifact is not serve-ready (%d completeness failures): %s",
			len(rep.HardFail), strings.Join(names, "; "))
	}
	return DetectFacts(dir)
}

// DetectFacts reads serving facts from a local model directory WITHOUT running
// the completeness gate. Callers that have not already verified the artifact
// through hfetch must use Verify instead; this is for paths where the gate has
// already run (e.g. an hfetch pull completed it at download time).
func DetectFacts(dir string) (serving.ArtifactFacts, error) {
	configPath := filepath.Join(dir, "config.json")
	configJSON, err := os.ReadFile(configPath)
	if err != nil {
		return serving.ArtifactFacts{}, fmt.Errorf("reading config.json: %w", err)
	}

	arch, err := firstArchitecture(configJSON)
	if err != nil {
		return serving.ArtifactFacts{}, err
	}

	facts := serving.ArtifactFacts{
		ModelPath:       dir,
		Arch:            arch,
		Tokenizer:       detectTokenizer(dir, configJSON),
		Quant:           detectQuant(dir, configJSON),
		HasVision:       hasVisionProcessor(dir),
		NeedsRemoteCode: needsRemoteCode(dir, configJSON),
	}
	return facts, nil
}

// firstArchitecture extracts config.json architectures[0] — the key the profile
// registry resolves on.
func firstArchitecture(configJSON []byte) (string, error) {
	var cfg struct {
		Architectures []string `json:"architectures"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return "", fmt.Errorf("parsing config.json: %w", err)
	}
	if len(cfg.Architectures) == 0 {
		return "", fmt.Errorf("config.json has no architectures")
	}
	return cfg.Architectures[0], nil
}

// detectTokenizer identifies the tokenizer/processor family from the artifact —
// a serving-relevant fact, since the tool parser and tokenizer-mode are
// family-specific. Tekken → mistral; a Qwen tokenizer_class → qwen; a
// SentencePiece model file → llama; otherwise generic.
func detectTokenizer(dir string, configJSON []byte) serving.TokenizerFamily {
	if fileExists(filepath.Join(dir, "tekken.json")) {
		return serving.TokenizerMistral
	}
	if cls := tokenizerClass(dir); cls != "" {
		lc := strings.ToLower(cls)
		switch {
		case strings.Contains(lc, "qwen"):
			return serving.TokenizerQwen
		case strings.Contains(lc, "llama"):
			return serving.TokenizerLlama
		}
	}
	// model_type in config.json is a secondary hint (e.g. "qwen3_moe").
	var cfg struct {
		ModelType string `json:"model_type"`
	}
	if json.Unmarshal(configJSON, &cfg) == nil {
		if strings.Contains(strings.ToLower(cfg.ModelType), "qwen") {
			return serving.TokenizerQwen
		}
	}
	if anyMatch(dir, "*.model") || anyMatch(dir, "*.spm") {
		return serving.TokenizerLlama
	}
	return serving.TokenizerGeneric
}

// tokenizerClass reads tokenizer_config.json's tokenizer_class, when present.
func tokenizerClass(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "tokenizer_config.json"))
	if err != nil {
		return ""
	}
	var tc struct {
		TokenizerClass string `json:"tokenizer_class"`
	}
	if json.Unmarshal(data, &tc) != nil {
		return ""
	}
	return tc.TokenizerClass
}

// detectQuant maps hfetch's quant detection onto the serving quant vocabulary,
// so the QuantFlags authority sees a known method. Reusing pkg/hfetch/quant
// keeps quant interpretation in one place across the toolchain.
//
// CRITICAL: quant metadata that is PRESENT but unrecognized must NOT collapse to
// a known-safe method (QuantNone / QuantNVFP4) — that would slip past the
// resolver's unknown-quant gate and emit a launch with no/wrong quant flag. An
// unrecognized method/algo is surfaced as a distinct, non-empty QuantMethod that
// is absent from QuantFlags, so contract.Resolve rejects it. Only a genuine
// absence of quant metadata (info == nil) is QuantNone.
func detectQuant(dir string, configJSON []byte) serving.QuantMethod {
	hfQuant, _ := os.ReadFile(filepath.Join(dir, "hf_quant_config.json"))
	gptq, _ := os.ReadFile(filepath.Join(dir, "quantize_config.json"))
	info := modelmeta.ParseQuant(configJSON, hfQuant, gptq)
	if info == nil {
		return serving.QuantNone // no quant metadata at all — unquantized
	}
	switch strings.ToLower(info.Method) {
	case "modelopt":
		switch strings.ToUpper(info.Algo) {
		case "NVFP4":
			return serving.QuantNVFP4
		case "FP8":
			return serving.QuantFP8
		default:
			// ModelOpt with an algo we have not validated a flag policy for.
			return unknownQuant("modelopt", info.Algo)
		}
	case "gptq":
		return serving.QuantGPTQ
	case "compressed-tensors":
		return serving.QuantCompressedTensors
	case "fp8":
		return serving.QuantFP8
	default:
		// A quant method present in the artifact that llm-serve does not know how
		// to serve (e.g. AWQ, a future family) — surface it so the resolver rejects.
		return unknownQuant(info.Method, info.Algo)
	}
}

// unknownQuant builds a non-empty, deliberately-unmapped QuantMethod label from
// detected metadata, so the resolver's unknown-quant gate rejects it with a
// legible name instead of silently emitting an un-flagged launch.
func unknownQuant(method, algo string) serving.QuantMethod {
	label := strings.ToLower(method)
	if algo != "" {
		label += "/" + strings.ToLower(algo)
	}
	if label == "" {
		label = "unknown"
	}
	return serving.QuantMethod(label)
}

// hasVisionProcessor reports whether the artifact ships a multimodal processor —
// the artifact-level fact the vision capability is gated on.
func hasVisionProcessor(dir string) bool {
	for _, f := range []string{
		"preprocessor_config.json", "processor_config.json", "video_preprocessor_config.json",
	} {
		if fileExists(filepath.Join(dir, f)) {
			return true
		}
	}
	return false
}

// needsRemoteCode reports whether the artifact requires --trust-remote-code: it
// ships modeling .py modules or names custom code via config.json auto_map.
func needsRemoteCode(dir string, configJSON []byte) bool {
	var cfg struct {
		AutoMap map[string]json.RawMessage `json:"auto_map"`
	}
	if json.Unmarshal(configJSON, &cfg) == nil && len(cfg.AutoMap) > 0 {
		return true
	}
	return anyMatch(dir, "modeling_*.py") || anyMatch(dir, "configuration_*.py")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func anyMatch(dir, pattern string) bool {
	m, err := filepath.Glob(filepath.Join(dir, pattern))
	return err == nil && len(m) > 0
}
