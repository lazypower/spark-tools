package fileset

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/lazypower/spark-tools/internal/hub"
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
// repoFiles is the repo's file tree (from hub.ListFiles, carrying canonical
// LFS hash + size); localDir is the flat directory the files were pulled into.
//
// It never returns an error for a merely-incomplete model — incompleteness is
// reported via Report.HardFail so callers can name every problem at once. A
// non-nil error means the gate itself could not run.
func Verify(repoFiles []hub.ModelFile, localDir string) (*Report, error) {
	rep := &Report{}

	// repoByBase keys flat files by base name (weights/configs live at the
	// root); repoByPath keys by full repo-relative path (needed for auto_map
	// modules that may sit in a subdirectory).
	repoByBase := make(map[string]hub.ModelFile, len(repoFiles))
	repoByPath := make(map[string]hub.ModelFile, len(repoFiles))
	var safetensors []string
	for _, f := range repoFiles {
		if f.Type == "directory" {
			continue
		}
		base := path.Base(f.Filename)
		repoByBase[base] = f
		repoByPath[f.Filename] = f
		if strings.HasSuffix(base, ".safetensors") {
			safetensors = append(safetensors, base)
		}
	}

	verifyWeights(rep, repoByBase, safetensors, localDir)

	// config.json — always required.
	checkRequired(rep, repoByBase, localDir, "config.json")

	// Tokenizer — at least one variant present, AND every present variant
	// verified intact (a corrupt tokenizer is not serve-ready).
	verifyTokenizers(rep, repoByBase, localDir)

	// Quant metadata — required iff the repo tree ships it.
	for _, q := range []string{"hf_quant_config.json", "quantize_config.json"} {
		if _, inRepo := repoByBase[q]; inRepo {
			checkRequired(rep, repoByBase, localDir, q)
		}
	}

	// Custom code named by config.json auto_map — must have landed AND be
	// intact. (Glob-all .py is the include mechanism; auto_map is the extra
	// assertion. It never names reasoning-parser plugins, so it is an
	// assertion, not the source.) Modules are checked at their full repo path
	// — a subdir module that the flat download collapsed fails closed rather
	// than passing on a flattened basename. External "repo--module" references
	// are excluded by autoMapModules (their code lives in another repo).
	for _, pyFile := range autoMapModules(filepath.Join(localDir, "config.json")) {
		f, inRepo := repoByPath[pyFile]
		verifyFile(rep, f, inRepo, filepath.Join(localDir, filepath.FromSlash(pyFile)), pyFile)
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
func verifyWeights(rep *Report, repoByBase map[string]hub.ModelFile, safetensors []string, localDir string) {
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

// checkRequired verifies one required flat (root-level) file by base name.
func checkRequired(rep *Report, repoByBase map[string]hub.ModelFile, localDir, base string) {
	f, inRepo := repoByBase[base]
	verifyFile(rep, f, inRepo, filepath.Join(localDir, base), base)
}

// verifyFile is the verification core: the file must be in the repo listing,
// present at localPath, size-matched, and content-matched — SHA256 for LFS
// files (oid is the content hash), git-blob SHA1 for non-LFS git files (oid is
// the blob id). name is the display label. Each distinct failure appends a
// named hard-fail Issue.
func verifyFile(rep *Report, f hub.ModelFile, inRepo bool, localPath, name string) {
	if !inRepo {
		rep.HardFail = append(rep.HardFail, Issue{name, "required file not in repo"})
		return
	}

	st, err := os.Stat(localPath)
	if err != nil {
		rep.HardFail = append(rep.HardFail, Issue{name, "missing locally"})
		return
	}

	wantSize := f.Size
	if f.LFS != nil {
		wantSize = f.LFS.Size
	}
	if wantSize > 0 && st.Size() != wantSize {
		rep.HardFail = append(rep.HardFail, Issue{name,
			fmt.Sprintf("size mismatch: have %d bytes, want %d", st.Size(), wantSize)})
		return
	}

	// LFS files: oid is the content SHA256. Non-LFS git files: oid is a git
	// blob SHA1, so verify content against that instead — never compare the
	// two hash types. (This is what caught hfetch's own 0-byte download bug.)
	switch {
	case f.LFS != nil && f.LFS.OID != "":
		got, err := hashFile(localPath)
		if err != nil {
			rep.HardFail = append(rep.HardFail, Issue{name, "hash read error: " + err.Error()})
			return
		}
		if got != f.LFS.OID {
			rep.HardFail = append(rep.HardFail, Issue{name, "SHA256 mismatch vs upstream (corrupt or truncated)"})
		}
	case isGitSHA1(f.BlobID):
		got, err := gitBlobSHA1(localPath)
		if err != nil {
			rep.HardFail = append(rep.HardFail, Issue{name, "hash read error: " + err.Error()})
			return
		}
		if got != f.BlobID {
			rep.HardFail = append(rep.HardFail, Issue{name, "git blob SHA1 mismatch vs upstream (corrupt or truncated)"})
		}
	}
}

// gitBlobSHA1 computes a file's git blob object id: SHA1("blob <size>\x00" +
// content). This is the oid HuggingFace reports for non-LFS files, so it lets
// the gate verify their content (catching the empty/truncated-file case)
// without a content SHA256, which HF does not provide for git-stored files.
func gitBlobSHA1(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isGitSHA1 reports whether s looks like a git object id (40 hex chars).
func isGitSHA1(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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

// verifyTokenizers enforces the tokenizer rule against the repo listing (the
// authority): every tokenizer variant the repo ships must be present locally
// and intact, and the repo must ship at least one. Keying off the repo — not
// local files — means a stale tokenizer left in a reused output dir can't
// satisfy the requirement for a model whose repo supplied none.
func verifyTokenizers(rep *Report, repoByBase map[string]hub.ModelFile, localDir string) {
	repoTokenizers := 0
	for base := range repoByBase {
		if isTokenizerFile(base) {
			repoTokenizers++
			checkRequired(rep, repoByBase, localDir, base)
		}
	}
	if repoTokenizers == 0 {
		rep.HardFail = append(rep.HardFail, Issue{"tokenizer", "no tokenizer file in repo (model is not independently serve-ready)"})
	}
}

func isTokenizerFile(base string) bool {
	return slices.Contains(tokenizerFiles, base) ||
		strings.HasSuffix(base, ".model") || strings.HasSuffix(base, ".spm")
}

// autoMapModules reads config.json's auto_map and returns the .py files its
// referenced modules resolve to (e.g. "modeling_x.XForCausalLM" → modeling_x.py).
// auto_map values may be a string ("module.Class") or an array of such strings
// (HF uses ["fast.Class", null] forms, notably for tokenizers) — both are
// handled so a missing modeling module is never silently skipped. Returns nil
// when config.json is absent or has no auto_map.
func autoMapModules(configPath string) []string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var cfg struct {
		AutoMap map[string]json.RawMessage `json:"auto_map"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		// "repo--module.Class" references code hosted in ANOTHER repo, fetched
		// separately by Transformers — it is not a file in THIS repo, so it is
		// not a local completeness requirement.
		if strings.Contains(v, "--") {
			return
		}
		dot := strings.LastIndex(v, ".")
		if dot <= 0 {
			return
		}
		// The module path before the class may use "." or "/" as separator;
		// normalize to a repo-relative .py path (subdir modules preserved).
		file := strings.ReplaceAll(v[:dot], ".", "/") + ".py"
		if !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
	}

	for _, raw := range cfg.AutoMap {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			add(s)
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			for _, e := range arr {
				var es string
				if json.Unmarshal(e, &es) == nil && es != "" {
					add(es)
				}
			}
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

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func anyLocalMatch(localDir, pattern string) bool {
	matches, err := filepath.Glob(filepath.Join(localDir, pattern))
	return err == nil && len(matches) > 0
}
