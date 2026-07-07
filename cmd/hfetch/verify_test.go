package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// treeServer returns a mock HF API serving the given recursive tree JSON for
// any request, plus a client pointed at it.
func treeServer(t *testing.T, treeJSON string) (*httptest.Server, *api.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, treeJSON)
	}))
	t.Cleanup(srv.Close)
	return srv, api.NewClient(api.WithBaseURL(srv.URL), api.WithToken("t"))
}

// emptyRegistry returns a loaded registry with no models — for verifying a
// model by directory (the safetensors gate path) without a registry entry.
func emptyRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestVerifyOne_CompleteModel_Passes(t *testing.T) {
	dir := t.TempDir()
	content := "the-weights"
	for name, body := range map[string]string{
		"model.safetensors":      content,
		"config.json":            `{}`,
		"tokenizer.json":         `{}`,
		"generation_config.json": `{}`,
		"chat_template.jinja":    `x`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(content))
	tree := fmt.Sprintf(`[
	  {"type":"file","path":"model.safetensors","lfs":{"oid":"%s","size":%d}},
	  {"type":"file","path":"config.json","size":2},
	  {"type":"file","path":"tokenizer.json","size":2},
	  {"type":"file","path":"generation_config.json","size":2},
	  {"type":"file","path":"chat_template.jinja","size":1}
	]`, hex.EncodeToString(sum[:]), len(content))

	_, client := treeServer(t, tree)
	if err := verifyOne(context.Background(), client, emptyRegistry(t), "org/model", dir); err != nil {
		t.Fatalf("complete model should verify clean: %v", err)
	}
}

func TestVerifyOne_Bitrot_Fails(t *testing.T) {
	dir := t.TempDir()
	// On-disk bytes differ from the canonical hash the server reports.
	os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("rotted"), 0644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0644)
	os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{}`), 0644)
	canonical := sha256.Sum256([]byte("pristine"))
	tree := fmt.Sprintf(`[
	  {"type":"file","path":"model.safetensors","lfs":{"oid":"%s","size":6}},
	  {"type":"file","path":"config.json","size":2},
	  {"type":"file","path":"tokenizer.json","size":2}
	]`, hex.EncodeToString(canonical[:]))

	_, client := treeServer(t, tree)
	if err := verifyOne(context.Background(), client, emptyRegistry(t), "org/model", dir); err == nil {
		t.Fatal("bitrot (hash mismatch) must fail verification")
	}
}

func TestVerifyOne_NotDownloaded_Fails(t *testing.T) {
	_, client := treeServer(t, `[]`)
	err := verifyOne(context.Background(), client, emptyRegistry(t), "org/model", filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("verifying a non-existent local dir must fail")
	}
}

// A GGUF model is one downloaded quant out of a repo that ships many. Verify
// must re-hash the recorded .gguf at its LocalPath against upstream and pass —
// NOT run the safetensors gate (which would hard-fail on the missing weights)
// and NOT fail on the other, un-downloaded quants in the repo.
func TestVerifyOne_GGUF_Passes(t *testing.T) {
	dir := t.TempDir()
	content := "gguf-quant-bytes"
	ggufPath := filepath.Join(dir, "model.Q4_K_M.gguf")
	if err := os.WriteFile(ggufPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))

	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	reg.AddFile("org/model-GGUF", registry.LocalFile{
		Filename:     "model.Q4_K_M.gguf",
		LocalPath:    ggufPath,
		SHA256:       hex.EncodeToString(sum[:]),
		Quantization: "Q4_K_M",
		Complete:     true,
	})

	// The repo ships two quants; only Q4_K_M was downloaded. The gate would
	// hard-fail (no safetensors, no tokenizer); VerifyFiles must not.
	tree := fmt.Sprintf(`[
	  {"type":"file","path":"model.Q4_K_M.gguf","lfs":{"oid":"%s","size":%d}},
	  {"type":"file","path":"model.Q8_0.gguf","lfs":{"oid":"deadbeef","size":999}}
	]`, hex.EncodeToString(sum[:]), len(content))

	_, client := treeServer(t, tree)
	if err := verifyOne(context.Background(), client, reg, "org/model-GGUF", ""); err != nil {
		t.Fatalf("a downloaded GGUF quant should verify clean: %v", err)
	}
}

// A GGUF model that also recorded a few loose config files must still verify as
// GGUF (re-hash the .gguf) and NOT trip the safetensors completeness gate, which
// would hard-fail a GGUF-only repo.
func TestVerifyOne_GGUFWithLooseConfig_Passes(t *testing.T) {
	dir := t.TempDir()
	content := "gguf-quant-bytes"
	ggufPath := filepath.Join(dir, "model.Q4_K_M.gguf")
	if err := os.WriteFile(ggufPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))

	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	reg.AddFile("org/model-GGUF", registry.LocalFile{
		Filename: "model.Q4_K_M.gguf", LocalPath: ggufPath, SHA256: hex.EncodeToString(sum[:]), Complete: true,
	})
	reg.AddFile("org/model-GGUF", registry.LocalFile{
		Filename: "config.json", LocalPath: cfgPath, Complete: true,
	})

	tree := fmt.Sprintf(`[
	  {"type":"file","path":"model.Q4_K_M.gguf","lfs":{"oid":"%s","size":%d}},
	  {"type":"file","path":"config.json","size":2}
	]`, hex.EncodeToString(sum[:]), len(content))
	_, client := treeServer(t, tree)
	if err := verifyOne(context.Background(), client, reg, "org/model-GGUF", ""); err != nil {
		t.Fatalf("a GGUF model with a loose config file should verify clean: %v", err)
	}
}

// A recorded GGUF with an empty LocalPath must resolve under the registry's
// model dir, never a bare cwd-relative filename that could verify an unrelated
// file in the working directory.
func TestGGUFLocalPath_NeverFallsBackToCwd(t *testing.T) {
	// Empty LocalPath, no --output → model dir + base.
	if got := ggufLocalPath(registry.LocalFile{Filename: "sub/model.gguf"}, "", "/data/models/x"); got != filepath.Join("/data/models/x", "model.gguf") {
		t.Errorf("empty LocalPath must resolve under the model dir, got %q", got)
	}
	// --output wins.
	if got := ggufLocalPath(registry.LocalFile{Filename: "model.gguf", LocalPath: "/reg/model.gguf"}, "/out", "/data"); got != filepath.Join("/out", "model.gguf") {
		t.Errorf("--output must override, got %q", got)
	}
	// LocalPath used when present and no override.
	if got := ggufLocalPath(registry.LocalFile{Filename: "model.gguf", LocalPath: "/reg/model.gguf"}, "", "/data"); got != "/reg/model.gguf" {
		t.Errorf("recorded LocalPath must be used, got %q", got)
	}
}

// A GGUF file whose on-disk bytes no longer match the upstream oid (bitrot or a
// truncated download) must fail verification.
func TestVerifyOne_GGUF_Bitrot_Fails(t *testing.T) {
	dir := t.TempDir()
	ggufPath := filepath.Join(dir, "model.Q4_K_M.gguf")
	if err := os.WriteFile(ggufPath, []byte("rotted"), 0644); err != nil {
		t.Fatal(err)
	}
	canonical := sha256.Sum256([]byte("pristine"))

	reg := registry.New(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	reg.AddFile("org/model-GGUF", registry.LocalFile{
		Filename: "model.Q4_K_M.gguf", LocalPath: ggufPath, Complete: true,
	})

	tree := fmt.Sprintf(`[
	  {"type":"file","path":"model.Q4_K_M.gguf","lfs":{"oid":"%s","size":6}}
	]`, hex.EncodeToString(canonical[:]))

	_, client := treeServer(t, tree)
	if err := verifyOne(context.Background(), client, reg, "org/model-GGUF", ""); err == nil {
		t.Fatal("a bitrotted GGUF file must fail verification")
	}
}
