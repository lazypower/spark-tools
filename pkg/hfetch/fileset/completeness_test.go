package fileset

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// gitBlob returns the git blob object id (SHA1) of content — the oid HF reports
// for non-LFS files.
func gitBlob(content string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// plainWithOID writes a non-LFS file and returns a ModelFile carrying its git
// blob oid, as the repo tree listing would.
func plainWithOID(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return api.ModelFile{Type: "file", Filename: name, Size: int64(len(content)), BlobID: gitBlob(content)}
}

// writeLocal writes content to dir/name and returns an LFS ModelFile whose
// OID/Size match the bytes — i.e. a correctly-downloaded shard.
func writeLocal(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return api.ModelFile{
		Type:     "file",
		Filename: name,
		LFS:      &api.LFS{OID: hex.EncodeToString(sum[:]), Size: int64(len(content))},
	}
}

// plain writes a non-LFS file (config/tokenizer/py) and returns its ModelFile.
func plain(t *testing.T, dir, name, content string) api.ModelFile {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return api.ModelFile{Type: "file", Filename: name, Size: int64(len(content))}
}

func indexFor(t *testing.T, dir string, shards ...string) api.ModelFile {
	t.Helper()
	wm := `{"weight_map":{`
	for i, s := range shards {
		if i > 0 {
			wm += ","
		}
		wm += `"layer` + s + `":"` + s + `"`
	}
	wm += `}}`
	return plain(t, dir, indexName, wm)
}

func TestVerify_MultiShard_Complete(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model-00001-of-00002.safetensors", "shard-one"),
		writeLocal(t, dir, "model-00002-of-00002.safetensors", "shard-two"),
		indexFor(t, dir, "model-00001-of-00002.safetensors", "model-00002-of-00002.safetensors"),
		plain(t, dir, "config.json", `{"architectures":["X"]}`),
		plain(t, dir, "tokenizer.json", `{}`),
		plain(t, dir, "generation_config.json", `{}`),
		plain(t, dir, "chat_template.jinja", `{{ x }}`),
	}
	rep, err := Verify(repo, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Complete() {
		t.Fatalf("expected complete; hard fails: %v", rep.HardFail)
	}
}

func TestVerify_MissingShard_HardFailsNamed(t *testing.T) {
	dir := t.TempDir()
	good := writeLocal(t, dir, "model-00001-of-00002.safetensors", "shard-one")
	// Second shard is in the repo + index but NOT written locally.
	missing := api.ModelFile{Type: "file", Filename: "model-00002-of-00002.safetensors",
		LFS: &api.LFS{OID: "deadbeef", Size: 9}}
	repo := []api.ModelFile{
		good, missing,
		indexFor(t, dir, "model-00001-of-00002.safetensors", "model-00002-of-00002.safetensors"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if rep.Complete() {
		t.Fatal("expected hard-fail for missing shard")
	}
	if !hasFail(rep, "model-00002-of-00002.safetensors") {
		t.Errorf("missing shard not named in hard fails: %v", rep.HardFail)
	}
}

func TestVerify_SingleShard_NoIndex_Valid(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "the-whole-thing"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
		plain(t, dir, "generation_config.json", `{}`),
		plain(t, dir, "chat_template.jinja", `x`),
	}
	rep, _ := Verify(repo, dir)
	if !rep.Complete() {
		t.Fatalf("single-shard model should be complete; hard fails: %v", rep.HardFail)
	}
}

func TestVerify_MultiShard_NoIndex_Suspicious(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model-a.safetensors", "a"),
		writeLocal(t, dir, "model-b.safetensors", "b"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if rep.Complete() {
		t.Fatal("multiple safetensors without an index must hard-fail")
	}
}

func TestVerify_QuantConfigInRepoButMissingLocally_HardFails(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "w"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
		// hf_quant_config.json is in the repo tree but never written locally.
		{Type: "file", Filename: "hf_quant_config.json", Size: 10},
	}
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "hf_quant_config.json") {
		t.Errorf("quant config present in repo but missing locally must hard-fail: %v", rep.HardFail)
	}
}

func TestVerify_AutoMapModuleMissing_HardFails(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "w"),
		plain(t, dir, "config.json", `{"auto_map":{"AutoModelForCausalLM":"modeling_nemotron_h.NemotronHForCausalLM"}}`),
		plain(t, dir, "tokenizer.json", `{}`),
	}
	// modeling_nemotron_h.py is referenced by auto_map but not on disk.
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "modeling_nemotron_h.py") {
		t.Errorf("auto_map module missing must hard-fail: %v", rep.HardFail)
	}
}

func TestVerify_HashMismatch_HardFails(t *testing.T) {
	dir := t.TempDir()
	// File on disk says "corrupt" but the repo OID is for "correct".
	plain(t, dir, "model.safetensors", "corrupt")
	sum := sha256.Sum256([]byte("correct"))
	repo := []api.ModelFile{
		{Type: "file", Filename: "model.safetensors", LFS: &api.LFS{OID: hex.EncodeToString(sum[:]), Size: 7}},
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "model.safetensors") {
		t.Errorf("hash mismatch must hard-fail: %v", rep.HardFail)
	}
}

func TestVerify_MissingChatTemplate_WarnsNotFails(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "w"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer.json", `{}`),
		plain(t, dir, "generation_config.json", `{}`),
		// no .jinja, and tokenizer.json has no chat_template
	}
	rep, _ := Verify(repo, dir)
	if !rep.Complete() {
		t.Fatalf("missing chat template should warn, not fail: %v", rep.HardFail)
	}
	if !hasWarn(rep, "chat_template.jinja") {
		t.Errorf("expected a chat-template warning: %v", rep.Warnings)
	}
}

func TestVerify_EmbeddedChatTemplate_NoWarn(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "w"),
		plain(t, dir, "config.json", `{}`),
		plain(t, dir, "tokenizer_config.json", `{"chat_template":"{{ messages }}"}`),
		plain(t, dir, "generation_config.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if hasWarn(rep, "chat_template.jinja") {
		t.Errorf("embedded chat_template should suppress the warning: %v", rep.Warnings)
	}
}

func TestVerify_NoTokenizer_HardFails(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "w"),
		plain(t, dir, "config.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "tokenizer") {
		t.Errorf("missing all tokenizers must hard-fail: %v", rep.HardFail)
	}
}

func TestVerify_NonLFS_GitBlobMatch_Passes(t *testing.T) {
	dir := t.TempDir()
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "weights"),
		plainWithOID(t, dir, "config.json", `{"architectures":["X"]}`),
		plainWithOID(t, dir, "tokenizer.json", `{"v":1}`),
		plainWithOID(t, dir, "generation_config.json", `{}`),
		plainWithOID(t, dir, "chat_template.jinja", `{{ messages }}`),
	}
	rep, _ := Verify(repo, dir)
	if !rep.Complete() {
		t.Fatalf("correct non-LFS files should pass git-blob verification: %v", rep.HardFail)
	}
}

func TestVerify_NonLFS_EmptyFile_HardFails(t *testing.T) {
	// The reported bug: a non-LFS file came down as 0 bytes. The gate must
	// fail closed and name it (here via size, with the git oid expecting content).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "weights"),
		{Type: "file", Filename: "config.json", Size: 7, BlobID: gitBlob(`{"a":1}`)},
		plainWithOID(t, dir, "tokenizer.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "config.json") {
		t.Errorf("empty non-LFS file must hard-fail (fail closed): %v", rep.HardFail)
	}
}

func TestVerify_NonLFS_CorruptSameSize_HardFails(t *testing.T) {
	// Same byte length, different content — only the git-blob hash catches this.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("BBBBBBBB"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := []api.ModelFile{
		writeLocal(t, dir, "model.safetensors", "weights"),
		{Type: "file", Filename: "config.json", Size: 8, BlobID: gitBlob("AAAAAAAA")},
		plainWithOID(t, dir, "tokenizer.json", `{}`),
	}
	rep, _ := Verify(repo, dir)
	if !hasFail(rep, "config.json") {
		t.Errorf("content-corrupt non-LFS file (same size) must hard-fail via git blob SHA1: %v", rep.HardFail)
	}
}

func hasFail(r *Report, file string) bool {
	for _, i := range r.HardFail {
		if i.File == file {
			return true
		}
	}
	return false
}

func hasWarn(r *Report, file string) bool {
	for _, i := range r.Warnings {
		if i.File == file {
			return true
		}
	}
	return false
}
