package fileset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// Issue is one completeness problem with a named file and a human reason.
type Issue struct {
	File   string
	Reason string
}

func (i Issue) String() string { return i.File + ": " + i.Reason }

// Report is the outcome of the completeness gate. A model is serve-ready only
// when HardFail is empty; Warnings are advisory (the model usually still loads).
type Report struct {
	HardFail []Issue
	Warnings []Issue
}

// Complete reports whether the model passed the gate (no hard failures).
func (r *Report) Complete() bool { return len(r.HardFail) == 0 }

const indexName = "model.safetensors.index.json"

// tokenizerFiles is the set of tokenizer variants; a model needs at least one.
var tokenizerFiles = []string{
	"tokenizer.json", "tokenizer_config.json", "tekken.json",
	"vocab.json", "merges.txt", "special_tokens_map.json", "added_tokens.json",
}

// Verify runs the completeness gate (spec §14.4) against an on-disk model.
// repoFiles is the repo's file tree (from api.ListFiles, carrying canonical
// LFS hash + size); localDir is the flat directory the files were pulled into.
//
// It never returns an error for a merely-incomplete model — incompleteness is
// reported via Report.HardFail so callers can name every problem at once. A
// non-nil error means the gate itself could not run.
func Verify(repoFiles []api.ModelFile, localDir string) (*Report, error) {
	rep := &Report{}

	repoByBase := make(map[string]api.ModelFile, len(repoFiles))
	var safetensors []string
	for _, f := range repoFiles {
		if f.Type == "directory" {
			continue
		}
		base := path.Base(f.Filename)
		repoByBase[base] = f
		if strings.HasSuffix(base, ".safetensors") {
			safetensors = append(safetensors, base)
		}
	}

	verifyWeights(rep, repoByBase, safetensors, localDir)

	// config.json — always required.
	checkRequired(rep, repoByBase, localDir, "config.json")

	// Tokenizer — at least one variant must be present locally.
	if !hasAnyTokenizer(localDir) {
		rep.HardFail = append(rep.HardFail, Issue{"tokenizer", "no tokenizer file present (need at least one)"})
	}

	// Quant metadata — required iff the repo tree ships it.
	for _, q := range []string{"hf_quant_config.json", "quantize_config.json"} {
		if _, inRepo := repoByBase[q]; inRepo {
			checkRequired(rep, repoByBase, localDir, q)
		}
	}

	// Custom code named by config.json auto_map — must have landed. (Glob-all
	// .py is the include mechanism; auto_map is the extra assertion. It never
	// names reasoning-parser plugins, so it is an assertion, not the source.)
	for _, pyFile := range autoMapModules(filepath.Join(localDir, "config.json")) {
		if !fileExists(filepath.Join(localDir, pyFile)) {
			rep.HardFail = append(rep.HardFail, Issue{pyFile, "auto_map module missing (trust-remote-code won't load)"})
		}
	}

	// Chat template — warn only; may be embedded in tokenizer_config.json.
	if !anyLocalMatch(localDir, "*.jinja") && !tokenizerHasChatTemplate(filepath.Join(localDir, "tokenizer_config.json")) {
		rep.Warnings = append(rep.Warnings, Issue{"chat_template.jinja", "absent and not embedded in tokenizer_config.json"})
	}

	// Generation config — warn only.
	if !fileExists(filepath.Join(localDir, "generation_config.json")) {
		rep.Warnings = append(rep.Warnings, Issue{"generation_config.json", "missing (sampling defaults)"})
	}

	return rep, nil
}

// verifyWeights resolves the required weight set and verifies each shard.
//
//	index present      → required = weight_map values ∪ all *.safetensors
//	                     (glob, don't derive solely — extra weights like an
//	                     MTP head are real and index-absent)
//	index absent, one  → single-shard model (valid, e.g. Devstral)
//	index absent, many → suspicious, hard-fail
func verifyWeights(rep *Report, repoByBase map[string]api.ModelFile, safetensors []string, localDir string) {
	_, hasIndex := repoByBase[indexName]

	required := map[string]bool{}
	switch {
	case hasIndex:
		checkRequired(rep, repoByBase, localDir, indexName)
		wm, err := readWeightMap(filepath.Join(localDir, indexName))
		if err != nil {
			// Can't derive the shard set; report and fall back to the glob.
			rep.HardFail = append(rep.HardFail, Issue{indexName, "unreadable: " + err.Error()})
		}
		for _, shard := range wm {
			required[path.Base(shard)] = true
		}
		for _, s := range safetensors {
			required[s] = true
		}
	case len(safetensors) == 1:
		required[safetensors[0]] = true
	case len(safetensors) == 0:
		rep.HardFail = append(rep.HardFail, Issue{"*.safetensors", "no safetensors weights and no index in repo"})
		return
	default:
		rep.HardFail = append(rep.HardFail, Issue{indexName,
			fmt.Sprintf("%d .safetensors files but no index — cannot determine completeness", len(safetensors))})
		return
	}

	shards := make([]string, 0, len(required))
	for s := range required {
		shards = append(shards, s)
	}
	sort.Strings(shards)
	for _, s := range shards {
		checkRequired(rep, repoByBase, localDir, s)
	}
}

// checkRequired verifies one required file: present in the repo, present
// locally, size-matched, and (for LFS files) hash-matched against the canonical
// upstream SHA256. Each distinct failure appends a named hard-fail Issue.
func checkRequired(rep *Report, repoByBase map[string]api.ModelFile, localDir, base string) {
	f, inRepo := repoByBase[base]
	if !inRepo {
		rep.HardFail = append(rep.HardFail, Issue{base, "required file not in repo"})
		return
	}

	lp := filepath.Join(localDir, base)
	st, err := os.Stat(lp)
	if err != nil {
		rep.HardFail = append(rep.HardFail, Issue{base, "missing locally"})
		return
	}

	wantSize := f.Size
	if f.LFS != nil {
		wantSize = f.LFS.Size
	}
	if wantSize > 0 && st.Size() != wantSize {
		rep.HardFail = append(rep.HardFail, Issue{base,
			fmt.Sprintf("size mismatch: have %d bytes, want %d", st.Size(), wantSize)})
		return
	}

	// Hash-verify only LFS files: their oid is the content SHA256. Non-LFS
	// small files carry a git blob oid, so presence + size is the check.
	if f.LFS != nil && f.LFS.OID != "" {
		got, err := hashFile(lp)
		if err != nil {
			rep.HardFail = append(rep.HardFail, Issue{base, "hash read error: " + err.Error()})
			return
		}
		if got != f.LFS.OID {
			rep.HardFail = append(rep.HardFail, Issue{base, "SHA256 mismatch vs upstream (corrupt or truncated)"})
		}
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readWeightMap(indexPath string) ([]string, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var idx struct {
		WeightMap map[string]string `json:"weight_map"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if len(idx.WeightMap) == 0 {
		return nil, fmt.Errorf("empty weight_map")
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range idx.WeightMap {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// autoMapModules reads config.json's auto_map and returns the .py files its
// referenced modules resolve to (e.g. "modeling_x.XForCausalLM" → modeling_x.py).
// Returns nil when config.json is absent or has no auto_map.
func autoMapModules(configPath string) []string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var cfg struct {
		AutoMap map[string]string `json:"auto_map"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range cfg.AutoMap {
		// Values may be "repo--module.Class"; the local module is after "--".
		if i := strings.Index(v, "--"); i >= 0 {
			v = v[i+2:]
		}
		dot := strings.LastIndex(v, ".")
		if dot <= 0 {
			continue
		}
		module := v[:dot]
		file := strings.ReplaceAll(module, ".", "/") + ".py"
		if !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
	}
	return out
}

func tokenizerHasChatTemplate(tokenizerConfigPath string) bool {
	data, err := os.ReadFile(tokenizerConfigPath)
	if err != nil {
		return false
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	raw, ok := cfg["chat_template"]
	return ok && len(raw) > 0 && string(raw) != "null" && string(raw) != `""`
}

func hasAnyTokenizer(localDir string) bool {
	for _, t := range tokenizerFiles {
		if fileExists(filepath.Join(localDir, t)) {
			return true
		}
	}
	return anyLocalMatch(localDir, "*.model") || anyLocalMatch(localDir, "*.spm")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func anyLocalMatch(localDir, pattern string) bool {
	matches, err := filepath.Glob(filepath.Join(localDir, pattern))
	return err == nil && len(matches) > 0
}
