package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

func ollamaImportCmd() *cobra.Command {
	var (
		name   string
		dryRun bool
		keep   bool
	)

	cmd := &cobra.Command{
		Use:   "ollama-import <model-ref>",
		Short: "Import a downloaded model into Ollama",
		Long: `Import a GGUF model from the hfetch registry into Ollama.

Model reference formats:
  org/model           Use the only available quantization
  org/model:Q4_K_M    Specify quantization
  /path/to/file.gguf  Use a local file directly

Split GGUF shards are automatically merged in pure Go before import.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOllamaImport(args[0], name, dryRun, keep)
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Override the Ollama model name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the Modelfile and command without executing")
	cmd.Flags().BoolVar(&keep, "keep", false, "Keep the merged GGUF file after import (split models only)")
	return cmd
}

func runOllamaImport(modelRef, ollamaName string, dryRun, keep bool) error {
	// Check ollama is available (skip for dry-run).
	if !dryRun {
		if _, err := exec.LookPath("ollama"); err != nil {
			return fmt.Errorf("ollama not found on PATH\n  Install: https://ollama.com/download")
		}
	}

	modelPath, modelID, quant, err := resolveModelFile(modelRef)
	if err != nil {
		return err
	}

	// Derive Ollama model name if not overridden.
	if ollamaName == "" {
		ollamaName = deriveOllamaName(modelID, quant)
	}

	// Extract chat template and stop tokens from GGUF metadata.
	ggufMeta := extractGGUFModelfileMeta(modelPath)

	// Generate Modelfile content.
	modelfileContent := buildModelfile(modelPath, ggufMeta)

	if dryRun {
		fmt.Printf("Model:  %s\n", modelPath)
		fmt.Printf("Name:   %s\n", ollamaName)
		if ggufMeta.EOS != "" {
			fmt.Printf("EOS:    %s\n", ggufMeta.EOS)
		}
		if ggufMeta.EOT != "" {
			fmt.Printf("EOT:    %s\n", ggufMeta.EOT)
		}
		if len(ggufMeta.StopTokens) > 0 {
			fmt.Printf("Stops:  %s\n", strings.Join(ggufMeta.StopTokens, ", "))
		}
		if ggufMeta.ChatTemplate != "" {
			goTmpl := detectGoTemplate(ggufMeta.ChatTemplate, ggufMeta.EOS)
			if goTmpl != "" {
				fmt.Printf("Template: detected family, emitting Go template\n")
			} else {
				fmt.Printf("Template: present in GGUF (%d bytes), NO family detected\n", len(ggufMeta.ChatTemplate))
				// Show first 500 chars to help debug template detection.
				preview := ggufMeta.ChatTemplate
				if len(preview) > 500 {
					preview = preview[:500] + "..."
				}
				fmt.Printf("Template preview:\n%s\n", preview)
			}
		} else {
			fmt.Printf("Template: not found in GGUF\n")
		}
		fmt.Printf("\nModelfile:\n%s\n", modelfileContent)
		fmt.Printf("Command:\n  ollama create %s -f <modelfile>\n", ollamaName)
		return nil
	}

	// Write Modelfile to a temp file.
	tmpFile, err := os.CreateTemp("", "hfetch-modelfile-*")
	if err != nil {
		return fmt.Errorf("creating temp modelfile: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(modelfileContent); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Run ollama create.
	fmt.Printf("  Importing %s into Ollama as %q...\n", filepath.Base(modelPath), ollamaName)
	ollamaCmd := exec.Command("ollama", "create", ollamaName, "-f", tmpFile.Name())
	ollamaCmd.Stdout = os.Stdout
	ollamaCmd.Stderr = os.Stderr
	if err := ollamaCmd.Run(); err != nil {
		return fmt.Errorf("ollama create failed: %w", err)
	}

	fmt.Printf("  Done! Run with: ollama run %s\n", ollamaName)

	// Clean up merged file if not keeping.
	if !keep && strings.HasSuffix(filepath.Base(modelPath), "-merged.gguf") {
		os.Remove(modelPath)
		fmt.Printf("  Cleaned up merged file: %s\n", filepath.Base(modelPath))
	}

	return nil
}

// resolveModelFile resolves a model reference to a local GGUF file path.
// Returns (filePath, modelID, quant, error).
func resolveModelFile(ref string) (string, string, string, error) {
	// Local file path.
	if isLocalPath(ref) {
		if _, err := os.Stat(ref); err != nil {
			return "", "", "", fmt.Errorf("file not found: %s", ref)
		}
		quant := gguf.ParseQuantFromFilename(ref)
		return ref, filepath.Base(ref), quant, nil
	}

	// Parse model-ref into model ID and optional quant.
	modelID, quant := parseModelRef(ref)

	dirs := config.Dirs()
	reg := registry.New(dirs.Data)
	if err := reg.Load(); err != nil {
		return "", "", "", err
	}

	model := reg.Get(modelID)
	if model == nil {
		return "", "", "", fmt.Errorf("model %q not found locally\n  Run: hfetch pull %s", modelID, modelID)
	}

	// Filter to complete GGUF files.
	var ggufFiles []registry.LocalFile
	for _, f := range model.Files {
		if f.Complete && gguf.IsGGUF(f.Filename) {
			ggufFiles = append(ggufFiles, f)
		}
	}
	if len(ggufFiles) == 0 {
		return "", "", "", fmt.Errorf("no complete GGUF files found for %s", modelID)
	}

	// Filter by quant if specified.
	if quant != "" {
		var matched []registry.LocalFile
		for _, f := range ggufFiles {
			if strings.EqualFold(f.Quantization, quant) {
				matched = append(matched, f)
			}
		}
		if len(matched) == 0 {
			available := availableQuants(ggufFiles)
			return "", "", "", fmt.Errorf("quantization %q not found for %s\n  Available: %s",
				quant, modelID, strings.Join(available, ", "))
		}
		ggufFiles = matched
	} else {
		// No quant specified — check for ambiguity.
		quants := availableQuants(ggufFiles)
		if len(quants) > 1 {
			return "", "", "", fmt.Errorf("multiple quantizations available for %s — specify one:\n  %s\n  Example: hfetch ollama-import %s:%s",
				modelID, strings.Join(quants, "\n  "), modelID, quants[0])
		}
		quant = ggufFiles[0].Quantization
	}

	// Check if this is a split model (multiple files for the same quant).
	if len(ggufFiles) > 1 {
		return handleSplitModel(ggufFiles, modelID, quant, dirs.Data)
	}

	return ggufFiles[0].LocalPath, modelID, quant, nil
}

// handleSplitModel merges split GGUF shards and returns the merged file path.
func handleSplitModel(files []registry.LocalFile, modelID, quant, dataDir string) (string, string, string, error) {
	// Sort shards by filename to ensure correct order.
	sort.Slice(files, func(i, j int) bool {
		return files[i].Filename < files[j].Filename
	})

	var shardPaths []string
	for _, f := range files {
		shardPaths = append(shardPaths, f.LocalPath)
	}

	// Check if a merged file already exists.
	mergedName := mergedFilename(files[0].Filename)
	mergedPath := filepath.Join(filepath.Dir(files[0].LocalPath), mergedName)
	if fi, err := os.Stat(mergedPath); err == nil && fi.Size() > 0 {
		fmt.Printf("  Using existing merged file: %s\n", mergedName)
		return mergedPath, modelID, quant, nil
	}

	// Merge shards.
	fmt.Printf("  Merging %d shards for %s %s...\n", len(files), modelID, quant)
	if err := gguf.MergeShards(shardPaths, mergedPath); err != nil {
		return "", "", "", fmt.Errorf("merging shards: %w", err)
	}
	fmt.Printf("  Merged: %s\n", mergedName)

	return mergedPath, modelID, quant, nil
}

func parseModelRef(ref string) (modelID, quant string) {
	if i := strings.LastIndex(ref, ":"); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

func isLocalPath(ref string) bool {
	return strings.HasPrefix(ref, "/") ||
		strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "~/")
}

// deriveOllamaName creates an Ollama model name from a HuggingFace model ID.
// "org/Model-Name-GGUF:Q4_K_M" → "model-name:Q4_K_M"
func deriveOllamaName(modelID, quant string) string {
	name := modelID
	// Strip org prefix.
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// Strip -GGUF suffix.
	name = regexp.MustCompile(`(?i)-gguf$`).ReplaceAllString(name, "")
	name = strings.ToLower(name)

	if quant != "" {
		return name + ":" + quant
	}
	return name
}

// mergedFilename derives a merged output filename from a shard filename.
// "model-Q4_K_M-00001-of-00003.gguf" → "model-Q4_K_M-merged.gguf"
func mergedFilename(shardFilename string) string {
	re := regexp.MustCompile(`-\d{5}-of-\d{5}\.gguf$`)
	if re.MatchString(shardFilename) {
		return re.ReplaceAllString(shardFilename, "-merged.gguf")
	}
	return strings.TrimSuffix(shardFilename, ".gguf") + "-merged.gguf"
}

// ggufModelfileMeta holds metadata extracted from a GGUF file for Modelfile generation.
type ggufModelfileMeta struct {
	ChatTemplate string
	StopTokens   []string
	BOS          string
	EOS          string
	EOT          string // end-of-turn token (newer models)
}

// extractGGUFModelfileMeta parses a GGUF file's header to extract chat template
// and special token metadata. Returns zero-value on any error (best-effort).
func extractGGUFModelfileMeta(path string) ggufModelfileMeta {
	f, err := os.Open(path)
	if err != nil {
		return ggufModelfileMeta{}
	}
	defer f.Close()

	hdr, err := gguf.ParseShard(f)
	if err != nil {
		return ggufModelfileMeta{}
	}

	var meta ggufModelfileMeta
	// Build a token ID → string lookup from the vocabulary.
	var tokens []string
	for _, kv := range hdr.KVs {
		if kv.Key == "tokenizer.ggml.tokens" {
			if ta, ok := kv.Value.(gguf.TypedArray); ok {
				for _, v := range ta.Values {
					if s, ok := v.(string); ok {
						tokens = append(tokens, s)
					}
				}
			}
		}
	}

	resolveTokenID := func(kv gguf.KV) string {
		if id := toInt(kv.Value); id >= 0 && id < len(tokens) {
			return tokens[id]
		}
		return ""
	}

	for _, kv := range hdr.KVs {
		switch kv.Key {
		case "tokenizer.chat_template":
			if s, ok := kv.Value.(string); ok {
				meta.ChatTemplate = s
			}
		case "tokenizer.ggml.bos_token_id":
			meta.BOS = resolveTokenID(kv)
		case "tokenizer.ggml.eos_token_id":
			meta.EOS = resolveTokenID(kv)
		case "tokenizer.ggml.eot_token_id":
			meta.EOT = resolveTokenID(kv)
		}
	}

	// When a template family is detected, use its curated stop list
	// (EOS/EOT + family extras) instead of generic inference. Some tokens
	// like <|end|> are stop-like by name but serve as internal delimiters
	// in certain families (e.g., GPT-OSS channel blocks).
	if fam := detectTemplateFamily(meta.ChatTemplate, meta.EOS); fam != nil {
		seen := make(map[string]bool)
		add := func(tok string) {
			if tok != "" && !seen[tok] {
				meta.StopTokens = append(meta.StopTokens, tok)
				seen[tok] = true
			}
		}
		add(meta.EOS)
		add(meta.EOT)
		for _, s := range fam.extraStops {
			add(s)
		}
	} else {
		meta.StopTokens = inferStopTokens(meta.ChatTemplate, meta.EOS, meta.EOT, tokens)
	}

	return meta
}

// inferStopTokens extracts stop/end-of-turn tokens from GGUF metadata.
// Uses a two-tier approach:
//  1. Always include EOS and EOT tokens from the GGUF token ID fields
//  2. Scan the chat template for special tokens that match known end-of-turn patterns
func inferStopTokens(template, eos, eot string, tokens []string) []string {
	var stops []string
	seen := make(map[string]bool)

	add := func(tok string) {
		if tok != "" && !seen[tok] {
			stops = append(stops, tok)
			seen[tok] = true
		}
	}

	// Tier 1: always include EOS and EOT from GGUF token IDs.
	add(eos)
	add(eot)

	// Tier 2: scan template and vocab for end-of-turn markers.
	// Only include tokens whose names indicate termination.
	isStopLike := func(tok string) bool {
		lower := strings.ToLower(tok)
		return strings.Contains(lower, "end") ||
			strings.Contains(lower, "eot") ||
			strings.Contains(lower, "stop") ||
			strings.Contains(lower, "return") ||
			strings.Contains(lower, "eos") ||
			tok == "</s>"
	}

	// Check vocab tokens that appear in the template.
	if template != "" {
		for _, tok := range tokens {
			if len(tok) < 3 {
				continue
			}
			isSpecial := strings.HasPrefix(tok, "<|") ||
				(strings.HasPrefix(tok, "<") && strings.HasSuffix(tok, ">"))
			if isSpecial && isStopLike(tok) && strings.Contains(template, tok) {
				add(tok)
			}
		}
	}

	// Always check for <|endoftext|> in the vocab even if not in the template —
	// it's the universal GPT-family EOS that models emit at generation end.
	for _, tok := range tokens {
		if tok == "<|endoftext|>" {
			add(tok)
			break
		}
	}

	return stops
}

// buildModelfile generates an Ollama Modelfile from the model path and
// extracted GGUF metadata.
func buildModelfile(modelPath string, meta ggufModelfileMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", modelPath)

	// Ollama reads the Jinja2 chat template from the GGUF metadata and
	// converts it to its Go template format internally. However, for many
	// model families (GPT-OSS, newer Phi variants, etc.) this conversion
	// fails and Ollama falls back to {{ .Prompt }} (raw completion mode).
	//
	// To avoid runaway generation, detect the template family from special
	// tokens in the Jinja2 template and emit the corresponding Go template
	// that Ollama understands.
	if goTmpl := detectGoTemplate(meta.ChatTemplate, meta.EOS); goTmpl != "" {
		fmt.Fprintf(&b, "\nTEMPLATE \"\"\"%s\"\"\"\n\n", goTmpl)
	}

	for _, stop := range meta.StopTokens {
		fmt.Fprintf(&b, "PARAMETER stop \"%s\"\n", stop)
	}

	return b.String()
}

// templateFamily represents a known chat template pattern.
type templateFamily struct {
	// markers are special tokens that identify this family.
	// ALL must appear in the Jinja2 template for a match.
	markers []string
	// eosHint is an EOS token that uniquely identifies this family.
	// Used as a fallback when markers aren't found literally in complex
	// templates that construct tokens via Jinja2 macros.
	eosHint string
	// goTemplate is the Ollama Go template for this family.
	goTemplate string
	// extraStops are additional stop tokens implied by this template.
	extraStops []string
}

// knownTemplateFamilies maps special token patterns to Ollama Go templates.
// Order matters — first match wins. More specific patterns come first.
var knownTemplateFamilies = []templateFamily{
	{
		// Llama 3 / Llama 3.1+
		markers: []string{"<|start_header_id|>", "<|end_header_id|>", "<|eot_id|>"},
		goTemplate: `{{ if .System }}<|start_header_id|>system<|end_header_id|>

{{ .System }}<|eot_id|>{{ end }}{{ if .Prompt }}<|start_header_id|>user<|end_header_id|>

{{ .Prompt }}<|eot_id|>{{ end }}<|start_header_id|>assistant<|end_header_id|>

{{ .Response }}<|eot_id|>`,
		extraStops: []string{"<|eot_id|>"},
	},
	{
		// ChatML (Qwen, Yi, OpenChat, many fine-tunes)
		markers: []string{"<|im_start|>", "<|im_end|>"},
		goTemplate: `{{ if .System }}<|im_start|>system
{{ .System }}<|im_end|>
{{ end }}{{ if .Prompt }}<|im_start|>user
{{ .Prompt }}<|im_end|>
{{ end }}<|im_start|>assistant
{{ .Response }}<|im_end|>`,
		extraStops: []string{"<|im_end|>"},
	},
	{
		// Phi-3 / models using <|system|>, <|user|>, <|end|>
		markers: []string{"<|user|>", "<|end|>", "<|assistant|>"},
		goTemplate: `{{ if .System }}<|system|>
{{ .System }}<|end|>
{{ end }}{{ if .Prompt }}<|user|>
{{ .Prompt }}<|end|>
{{ end }}<|assistant|>
{{ .Response }}<|end|>`,
		extraStops: []string{"<|end|>", "<|endoftext|>"},
	},
	{
		// GPT-OSS — uses <|start|>role<|message|>content<|end|> format.
		// Uses .Messages range for multi-turn chat API support. Ends with
		// <|start|>assistant so the model emits its own channel routing
		// (analysis→final). Ollama's harmony parser routes analysis to
		// thinking and final to content. <|end|> must NOT be a stop token —
		// it closes internal channel blocks.
		markers: []string{"<|start|>", "<|end|>", "<|message|>"},
		eosHint: "<|return|>",
		goTemplate: `{{- if .System }}<|start|>system<|message|>{{ .System }}<|end|>{{ end }}
{{- range .Messages }}
{{- if eq .Role "user" }}<|start|>user<|message|>{{ .Content }}<|end|>{{ end }}
{{- if eq .Role "assistant" }}<|start|>assistant<|channel|>final<|message|>{{ .Content }}<|end|>{{ end }}
{{- end }}<|start|>assistant`,
		extraStops: []string{"<|endoftext|>"},
	},
	{
		// Gemma / Gemma 2
		markers: []string{"<start_of_turn>", "<end_of_turn>"},
		goTemplate: `{{ if .System }}<start_of_turn>user
{{ .System }}<end_of_turn>
{{ end }}<start_of_turn>user
{{ .Prompt }}<end_of_turn>
<start_of_turn>model
{{ .Response }}<end_of_turn>`,
		extraStops: []string{"<end_of_turn>"},
	},
	{
		// Mistral / Mixtral ([INST] style)
		markers: []string{"[INST]", "[/INST]"},
		goTemplate: `{{ if .System }}[INST] {{ .System }}

{{ .Prompt }} [/INST]{{ else }}[INST] {{ .Prompt }} [/INST]{{ end }}{{ .Response }}`,
		extraStops: []string{"[INST]", "</s>"},
	},
	{
		// DeepSeek
		markers: []string{"<|User|>", "<|Assistant|>"},
		goTemplate: `{{ if .System }}<|System|>{{ .System }}
{{ end }}<|User|>{{ .Prompt }}
<|Assistant|>{{ .Response }}`,
		extraStops: []string{"<|User|>", "<|end▁of▁sentence|>"},
	},
	{
		// Command R / Cohere
		markers: []string{"<|START_OF_TURN_TOKEN|>", "<|END_OF_TURN_TOKEN|>"},
		goTemplate: `{{ if .System }}<|START_OF_TURN_TOKEN|><|SYSTEM_TOKEN|>{{ .System }}<|END_OF_TURN_TOKEN|>{{ end }}<|START_OF_TURN_TOKEN|><|USER_TOKEN|>{{ .Prompt }}<|END_OF_TURN_TOKEN|><|START_OF_TURN_TOKEN|><|CHATBOT_TOKEN|>{{ .Response }}<|END_OF_TURN_TOKEN|>`,
		extraStops: []string{"<|END_OF_TURN_TOKEN|>"},
	},
}

// detectTemplateFamily examines the Jinja2 chat template and EOS token to
// identify the model's template family. Returns the family or nil if unknown.
func detectTemplateFamily(jinjaTemplate, eos string) *templateFamily {
	if jinjaTemplate == "" {
		return nil
	}

	// First: try matching by template markers (literal special tokens).
	for i := range knownTemplateFamilies {
		fam := &knownTemplateFamilies[i]
		match := true
		for _, m := range fam.markers {
			if !strings.Contains(jinjaTemplate, m) {
				match = false
				break
			}
		}
		if match {
			return fam
		}
	}

	// Second: use EOS token as a family fingerprint for complex templates
	// where markers are constructed via Jinja2 macros rather than literals.
	for i := range knownTemplateFamilies {
		fam := &knownTemplateFamilies[i]
		if fam.eosHint != "" && fam.eosHint == eos {
			return fam
		}
	}

	return nil
}

// detectGoTemplate returns the Ollama Go template for the detected family, or "".
func detectGoTemplate(jinjaTemplate, eos string) string {
	if fam := detectTemplateFamily(jinjaTemplate, eos); fam != nil {
		return fam.goTemplate
	}
	return ""
}

// toInt converts a GGUF value to an int index. Returns -1 on failure.
func toInt(v any) int {
	switch n := v.(type) {
	case uint32:
		return int(n)
	case int32:
		return int(n)
	case uint64:
		return int(n)
	case int64:
		return int(n)
	case uint16:
		return int(n)
	case int16:
		return int(n)
	default:
		return -1
	}
}

func availableQuants(files []registry.LocalFile) []string {
	seen := make(map[string]bool)
	var quants []string
	for _, f := range files {
		q := f.Quantization
		if !seen[q] {
			seen[q] = true
			quants = append(quants, q)
		}
	}
	sort.Strings(quants)
	return quants
}
