package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheTTL is the default time-to-live for cached API metadata.
const CacheTTL = 24 * time.Hour

// WithCacheDir sets the directory for caching API metadata.
func WithCacheDir(dir string) Option {
	return func(c *Client) { c.cacheDir = dir }
}

// cacheEntry wraps cached data with an expiration timestamp.
type cacheEntry struct {
	CachedAt time.Time       `json:"cached_at"`
	Data     json.RawMessage `json:"data"`
}

// cacheModelPath returns the cache file path for a model's metadata.
func (c *Client) cacheModelPath(modelID string) string {
	if c.cacheDir == "" {
		return ""
	}
	safeName := strings.ReplaceAll(modelID, "/", "--")
	return filepath.Join(c.cacheDir, "models", safeName, "meta.json")
}

// loadCache reads a cached entry if it exists and hasn't expired.
func (c *Client) loadCache(path string) (json.RawMessage, bool) {
	if path == "" {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Since(entry.CachedAt) > CacheTTL {
		return nil, false
	}

	return entry.Data, true
}

// saveCache writes data to the cache.
func (c *Client) saveCache(path string, data []byte) {
	if path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}

	entry := cacheEntry{
		CachedAt: time.Now(),
		Data:     json.RawMessage(data),
	}
	encoded, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, encoded, 0644)
}
