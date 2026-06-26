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
	if err := verifyOne(context.Background(), client, "org/model", dir); err != nil {
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
	if err := verifyOne(context.Background(), client, "org/model", dir); err == nil {
		t.Fatal("bitrot (hash mismatch) must fail verification")
	}
}

func TestApiFileSource_HeadUsesTreeMetadata(t *testing.T) {
	// Head must return the size/hash injected from the tree listing, never a
	// network HEAD — otherwise non-LFS files get size 0 (→ 0-byte download).
	// Client base URL points nowhere; if Head dialed out, this would error.
	src := &apiFileSource{
		client:  api.NewClient(api.WithBaseURL("http://127.0.0.1:0"), api.WithToken("t")),
		modelID: "org/model",
		file:    "config.json",
		size:    1234,
		sha256:  "", // non-LFS: no content hash
	}
	size, sha, err := src.Head(context.Background())
	if err != nil {
		t.Fatalf("Head should not error (no network): %v", err)
	}
	if size != 1234 || sha != "" {
		t.Errorf("Head should echo tree metadata, got size=%d sha=%q", size, sha)
	}
}

func TestVerifyOne_NotDownloaded_Fails(t *testing.T) {
	_, client := treeServer(t, `[]`)
	err := verifyOne(context.Background(), client, "org/model", filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("verifying a non-existent local dir must fail")
	}
}
