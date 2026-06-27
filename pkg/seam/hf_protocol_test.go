package seam

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
)

// Seam: hfetch's HF client (parser) <-> the real HuggingFace tree-listing
// protocol it stands in for.
//
// CONTRACT: the tree listing is the authority for file size + hash, and it
// distinguishes LFS files (lfs.oid = content SHA256, lfs.size = real size) from
// non-LFS git files (top-level oid = git blob SHA1, top-level size = real size,
// NO lfs object). Conflating the two is the bug class that shipped a 0-byte /
// SHA1-vs-SHA256 download. This fixture replicates the real response shape so a
// future change to ListFiles parsing (or a drifted mock) fails here.
//
// STATUS: GREEN — guards the fix.
func TestSeam_HFTreeListing_LFSvsNonLFS(t *testing.T) {
	// Shapes taken from real HuggingFace /api/models/<repo>/tree responses.
	const tree = `[
	  {"type":"file","path":"model-00001-of-00001.safetensors","size":135,
	   "oid":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
	   "lfs":{"oid":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","size":13000000000}},
	  {"type":"file","path":"config.json","size":684,
	   "oid":"b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tree/main") {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, tree)
	}))
	defer srv.Close()

	client := api.NewClient(api.WithBaseURL(srv.URL), api.WithToken("t"))
	files, err := client.ListFiles(context.Background(), "org/model")
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]api.ModelFile{}
	for _, f := range files {
		byName[f.Filename] = f
	}

	// LFS weight shard: must expose the content SHA256 + real size via lfs.
	shard, ok := byName["model-00001-of-00001.safetensors"]
	if !ok {
		t.Fatal("LFS shard missing from listing")
	}
	if shard.LFS == nil {
		t.Fatal("LFS shard parsed without an lfs object — would lose the content SHA256 and real size")
	}
	if len(shard.LFS.OID) != 64 {
		t.Errorf("LFS oid should be a content SHA256 (64 hex), got %q", shard.LFS.OID)
	}
	if shard.LFS.Size != 13000000000 {
		t.Errorf("LFS size should come from lfs.size, got %d", shard.LFS.Size)
	}

	// Non-LFS git file: no lfs object; oid is a git blob SHA1 (40 hex), size real.
	cfg, ok := byName["config.json"]
	if !ok {
		t.Fatal("non-LFS config.json missing from listing")
	}
	if cfg.LFS != nil {
		t.Error("non-LFS config.json must not have an lfs object")
	}
	if len(cfg.BlobID) != 40 {
		t.Errorf("non-LFS oid should be a git blob SHA1 (40 hex), got %q", cfg.BlobID)
	}
	if cfg.Size != 684 {
		t.Errorf("non-LFS size should come from the top-level size, got %d", cfg.Size)
	}
}

// CONTRACT: HeadFile returns a content hash ONLY for LFS files (X-Linked-Etag).
// For non-LFS files HF sends a git-blob ETag, which is NOT a content hash and
// must never be returned as one — that produced a guaranteed verify mismatch.
//
// STATUS: GREEN — guards the fix.
func TestSeam_HFHeadFile_NonLFSHasNoContentHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Real non-LFS HEAD: a git-blob ETag, no X-Linked-* headers.
		w.Header().Set("Content-Length", "684")
		w.Header().Set("ETag", `"b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(api.WithBaseURL(srv.URL), api.WithToken("t"))
	size, sha, err := client.HeadFile(context.Background(), "org/model", "config.json")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		t.Errorf("non-LFS HeadFile must not return a content hash (got the git ETag %q)", sha)
	}
	if size != 684 {
		t.Errorf("non-LFS HeadFile should still report size, got %d", size)
	}
}
