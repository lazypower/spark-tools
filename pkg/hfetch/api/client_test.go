package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypower/spark-tools/pkg/hfetch/auth"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	client := NewClient(WithBaseURL(srv.URL), WithToken("hf_test"))
	return srv, client
}

func TestWhoAmI(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/whoami" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer hf_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(auth.UserInfo{
			Username: "testuser",
			FullName: "Test User",
		})
	})
	defer srv.Close()

	info, err := client.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	if info.Username != "testuser" {
		t.Errorf("expected testuser, got %q", info.Username)
	}
}

func TestWhoAmI_NoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL)) // no token
	_, err := client.WhoAmI(context.Background())
	if !errors.Is(err, auth.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", err)
	}
}

func TestWhoAmI_InvalidToken(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	_, err := client.WhoAmI(context.Background())
	if !errors.Is(err, auth.ErrAuthInvalid) {
		t.Errorf("expected ErrAuthInvalid, got %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("search") != "llama" {
			t.Errorf("expected search=llama, got %q", q.Get("search"))
		}
		if q.Get("limit") != "5" {
			t.Errorf("expected limit=5, got %q", q.Get("limit"))
		}
		json.NewEncoder(w).Encode([]Model{
			{ID: "meta-llama/Llama-2-7B", Downloads: 1000},
			{ID: "TheBloke/Llama-2-7B-GGUF", Downloads: 500},
		})
	})
	defer srv.Close()

	models, err := client.Search(context.Background(), "llama", SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestGetModel(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/TheBloke/Llama-2-7B-GGUF" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(Model{
			ID:     "TheBloke/Llama-2-7B-GGUF",
			Author: "TheBloke",
		})
	})
	defer srv.Close()

	model, err := client.GetModel(context.Background(), "TheBloke/Llama-2-7B-GGUF")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	if model.ID != "TheBloke/Llama-2-7B-GGUF" {
		t.Errorf("expected TheBloke/Llama-2-7B-GGUF, got %q", model.ID)
	}
}

func TestListFiles(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/TheBloke/Llama-2-7B-GGUF/tree/main" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]ModelFile{
			{Filename: "llama-2-7b.Q4_K_M.gguf", Size: 4_000_000_000},
			{Filename: "llama-2-7b.Q8_0.gguf", Size: 7_000_000_000},
			{Filename: "README.md", Size: 1234},
		})
	})
	defer srv.Close()

	files, err := client.ListFiles(context.Background(), "TheBloke/Llama-2-7B-GGUF")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestGatedModel(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	defer srv.Close()

	_, err := client.GetModel(context.Background(), "meta-llama/Llama-2-7B")
	if !errors.Is(err, auth.ErrGatedModel) {
		t.Errorf("expected ErrGatedModel, got %v", err)
	}
}

func TestRetryOn500(t *testing.T) {
	attempts := 0
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(Model{ID: "test/model"})
	})
	defer srv.Close()

	model, err := client.GetModel(context.Background(), "test/model")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if model.ID != "test/model" {
		t.Errorf("expected test/model, got %q", model.ID)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// ---------------------------------------------------------------------------
// WithHTTPClient option
// ---------------------------------------------------------------------------

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	c := NewClient(WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Error("WithHTTPClient did not set the custom HTTP client")
	}
}

// ---------------------------------------------------------------------------
// HeadFile
// ---------------------------------------------------------------------------

func TestHeadFile(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD method, got %s", r.Method)
		}
		if r.URL.Path != "/test/model/resolve/main/weights.gguf" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "42000")
		w.Header().Set("X-Linked-Etag", "\"abc123sha\"")
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	size, sha, err := client.HeadFile(context.Background(), "test/model", "weights.gguf")
	if err != nil {
		t.Fatalf("HeadFile: %v", err)
	}
	if size != 42000 {
		t.Errorf("expected size 42000, got %d", size)
	}
	if sha != "abc123sha" {
		t.Errorf("expected sha abc123sha, got %q", sha)
	}
}

func TestHeadFile_FallbackETag(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Header().Set("ETag", "\"fallbackhash\"")
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	_, sha, err := client.HeadFile(context.Background(), "test/model", "file.bin")
	if err != nil {
		t.Fatalf("HeadFile: %v", err)
	}
	if sha != "fallbackhash" {
		t.Errorf("expected fallbackhash, got %q", sha)
	}
}

// ---------------------------------------------------------------------------
// FetchFileRange
// ---------------------------------------------------------------------------

func TestFetchFileRange(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test/model/resolve/main/header.gguf" {
			http.NotFound(w, r)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "bytes=0-4095" {
			t.Errorf("expected Range bytes=0-4095, got %q", rangeHeader)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("partial-content-data"))
	})
	defer srv.Close()

	data, err := client.FetchFileRange(context.Background(), "test/model", "header.gguf", 0, 4095)
	if err != nil {
		t.Fatalf("FetchFileRange: %v", err)
	}
	if string(data) != "partial-content-data" {
		t.Errorf("expected partial-content-data, got %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// DownloadFile
// ---------------------------------------------------------------------------

func TestDownloadFile(t *testing.T) {
	body := "file-content-here"
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test/model/resolve/main/model.bin" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") != "" {
			t.Errorf("expected no Range header for zero offset, got %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.Write([]byte(body))
	})
	defer srv.Close()

	rc, size, err := client.DownloadFile(context.Background(), "test/model", "model.bin", 0)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(data) != body {
		t.Errorf("expected %q, got %q", body, string(data))
	}
	if size != int64(len(body)) {
		t.Errorf("expected size %d, got %d", len(body), size)
	}
}

func TestDownloadFile_WithOffset(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "bytes=1024-" {
			t.Errorf("expected Range bytes=1024-, got %q", rangeHeader)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("remaining"))
	})
	defer srv.Close()

	rc, _, err := client.DownloadFile(context.Background(), "test/model", "model.bin", 1024)
	if err != nil {
		t.Fatalf("DownloadFile with offset: %v", err)
	}
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	if string(data) != "remaining" {
		t.Errorf("expected remaining, got %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// do() error paths — 404, other 4xx, rate-limit exhaustion, 500 exhaustion
// ---------------------------------------------------------------------------

func TestDo_NotFound(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()

	_, err := client.GetModel(context.Background(), "nonexistent/model")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestDo_OtherClientError(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request body"))
	})
	defer srv.Close()

	_, err := client.GetModel(context.Background(), "test/model")
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("expected 'HTTP 400' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "bad request body") {
		t.Errorf("expected body content in error, got %q", err.Error())
	}
}

func TestDo_RateLimitExhausted(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	// Use zero retries so we don't actually wait
	client := NewClient(WithBaseURL(srv.URL), WithToken("hf_test"))
	client.maxRetries = 0

	_, err := client.GetModel(context.Background(), "test/model")
	if err == nil {
		t.Fatal("expected error for exhausted rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected 'rate limited' in error, got %q", err.Error())
	}
}

func TestDo_ServerErrorExhausted(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL), WithToken("hf_test"))
	client.maxRetries = 0

	_, err := client.GetModel(context.Background(), "test/model")
	if err == nil {
		t.Fatal("expected error for exhausted server errors, got nil")
	}
	if !strings.Contains(err.Error(), "server error") {
		t.Errorf("expected 'server error' in error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Cache functions
// ---------------------------------------------------------------------------

func TestWithCacheDir(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))
	if c.cacheDir != dir {
		t.Errorf("expected cacheDir %q, got %q", dir, c.cacheDir)
	}
}

func TestCacheModelPath_WithDir(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))
	path := c.cacheModelPath("org/model-name")
	expected := filepath.Join(dir, "models", "org--model-name", "meta.json")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestCacheModelPath_NoDir(t *testing.T) {
	c := NewClient()
	path := c.cacheModelPath("org/model")
	if path != "" {
		t.Errorf("expected empty string when no cacheDir, got %q", path)
	}
}

func TestSaveAndLoadCache(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))

	modelID := "test/cached-model"
	model := Model{ID: modelID, Author: "tester"}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshaling model: %v", err)
	}

	path := c.cacheModelPath(modelID)
	c.saveCache(path, data)

	// Verify the file was actually written
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file does not exist: %v", err)
	}

	// Load it back
	loaded, ok := c.loadCache(path)
	if !ok {
		t.Fatal("loadCache returned not ok")
	}

	var got Model
	if err := json.Unmarshal(loaded, &got); err != nil {
		t.Fatalf("unmarshaling cached data: %v", err)
	}
	if got.ID != modelID {
		t.Errorf("expected cached model ID %q, got %q", modelID, got.ID)
	}
}

func TestLoadCache_Expired(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))

	path := filepath.Join(dir, "expired.json")
	entry := cacheEntry{
		CachedAt: time.Now().Add(-25 * time.Hour), // expired (>24h)
		Data:     json.RawMessage(`{"id":"old"}`),
	}
	encoded, _ := json.MarshalIndent(entry, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, encoded, 0644)

	_, ok := c.loadCache(path)
	if ok {
		t.Error("expected loadCache to reject expired entry")
	}
}

func TestLoadCache_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))

	path := filepath.Join(dir, "invalid.json")
	os.WriteFile(path, []byte("not valid json!!!"), 0644)

	_, ok := c.loadCache(path)
	if ok {
		t.Error("expected loadCache to reject invalid JSON")
	}
}

func TestLoadCache_MissingFile(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(WithCacheDir(dir))

	_, ok := c.loadCache(filepath.Join(dir, "does-not-exist.json"))
	if ok {
		t.Error("expected loadCache to return false for missing file")
	}
}

func TestLoadCache_EmptyPath(t *testing.T) {
	c := NewClient()
	_, ok := c.loadCache("")
	if ok {
		t.Error("expected loadCache to return false for empty path")
	}
}

func TestSaveCache_EmptyPath(t *testing.T) {
	c := NewClient()
	// Should not panic
	c.saveCache("", []byte(`{"id":"test"}`))
}

func TestGetModel_WithCache(t *testing.T) {
	dir := t.TempDir()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.NewEncoder(w).Encode(Model{ID: "cached/model", Author: "author"})
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL), WithToken("hf_test"), WithCacheDir(dir))

	// First call should hit the server
	model, err := client.GetModel(context.Background(), "cached/model")
	if err != nil {
		t.Fatalf("first GetModel: %v", err)
	}
	if model.ID != "cached/model" {
		t.Errorf("expected cached/model, got %q", model.ID)
	}
	if requests != 1 {
		t.Errorf("expected 1 request, got %d", requests)
	}

	// Second call should use the cache
	model, err = client.GetModel(context.Background(), "cached/model")
	if err != nil {
		t.Fatalf("second GetModel: %v", err)
	}
	if model.ID != "cached/model" {
		t.Errorf("expected cached/model from cache, got %q", model.ID)
	}
	if requests != 1 {
		t.Errorf("expected still 1 request (cache hit), got %d", requests)
	}
}

func TestSearch_WithFilterAndSort(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("filter") != "gguf" {
			t.Errorf("expected filter=gguf, got %q", q.Get("filter"))
		}
		if q.Get("sort") != "downloads" {
			t.Errorf("expected sort=downloads, got %q", q.Get("sort"))
		}
		json.NewEncoder(w).Encode([]Model{{ID: "test/model"}})
	})
	defer srv.Close()

	_, err := client.Search(context.Background(), "test", SearchOptions{
		Filter: "gguf",
		Sort:   "downloads",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "20" {
			t.Errorf("expected default limit=20, got %q", q.Get("limit"))
		}
		json.NewEncoder(w).Encode([]Model{})
	})
	defer srv.Close()

	_, err := client.Search(context.Background(), "test", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
}
