package hfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

func TestFileMeta(t *testing.T) {
	files := []ModelFile{
		{Type: "file", Filename: "weights.safetensors", Size: 0, LFS: &api.LFS{OID: "abc", Size: 4096}},
		{Type: "file", Filename: "config.json", Size: 12}, // non-LFS git file
	}
	// LFS file: size + hash come from the LFS block, NOT the (often-zero) Size.
	if size, sha, ok := fileMeta(files, "weights.safetensors"); !ok || size != 4096 || sha != "abc" {
		t.Errorf("LFS meta = %d,%q,%v; want 4096,abc,true", size, sha, ok)
	}
	// Non-LFS file: plain Size, empty hash.
	if size, sha, ok := fileMeta(files, "config.json"); !ok || size != 12 || sha != "" {
		t.Errorf("non-LFS meta = %d,%q,%v; want 12,'',true", size, sha, ok)
	}
	if _, _, ok := fileMeta(files, "absent.bin"); ok {
		t.Error("a missing file must report ok=false")
	}
}

// TestClient_Pull exercises the downstream import surface end to end against a
// mock Hub: it must resolve size/hash from the tree listing (the authority),
// download the bytes, write them to the output dir, and register the file.
func TestClient_Pull(t *testing.T) {
	content := []byte("safetensors-weights-payload")
	sum := sha256.Sum256(content)
	oid := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tree/main"):
			_ = json.NewEncoder(w).Encode([]ModelFile{
				{Type: "file", Filename: "model.safetensors", Size: 0,
					LFS: &api.LFS{OID: oid, Size: int64(len(content))}},
			})
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			w.Header().Set("Content-Length", "")
			_, _ = w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	t.Setenv("HFETCH_DATA_DIR", dataDir)
	t.Setenv("HFETCH_CACHE_DIR", t.TempDir())

	client, err := NewClient(WithBaseURL(srv.URL), WithToken("t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	out := t.TempDir()
	lf, err := client.Pull(context.Background(), "org/model", "model.safetensors", PullOptions{
		OutputDir: out,
		Streams:   1,
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Returned record reflects the tree-listing authority (size), not HEAD.
	if lf.Size != int64(len(content)) || !lf.Complete {
		t.Errorf("unexpected LocalFile: %+v", lf)
	}
	// Bytes landed on disk with the right content.
	got, err := os.ReadFile(lf.LocalPath)
	if err != nil || string(got) != string(content) {
		t.Fatalf("downloaded file wrong: err=%v content=%q", err, got)
	}
	if filepath.Dir(lf.LocalPath) != out {
		t.Errorf("file must land under OutputDir %q, got %q", out, lf.LocalPath)
	}
	// The file is registered and persisted.
	if lm := client.Registry().Get("org/model"); lm == nil || len(lm.Files) != 1 {
		t.Errorf("pulled file must be registered, got %+v", lm)
	}
}

func TestClient_Pull_FileNotInListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tree/main") {
			_ = json.NewEncoder(w).Encode([]ModelFile{{Type: "file", Filename: "other.bin", Size: 1}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("HFETCH_DATA_DIR", t.TempDir())
	t.Setenv("HFETCH_CACHE_DIR", t.TempDir())
	client, err := NewClient(WithBaseURL(srv.URL), WithToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Pull(context.Background(), "org/model", "ghost.gguf", PullOptions{OutputDir: t.TempDir(), Streams: 1}); err == nil {
		t.Error("pulling a file absent from the tree listing must error (no silent 0-byte file)")
	}
}
