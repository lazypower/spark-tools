// Package ollama is a minimal HTTP client for the Ollama REST API.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultHost is the Ollama server address used when OLLAMA_HOST is unset.
const DefaultHost = "http://localhost:11434"

// EnvHost is the environment variable Ollama itself reads for its address.
const EnvHost = "OLLAMA_HOST"

// Client is a minimal Ollama REST client.
type Client struct {
	host string
	http *http.Client
}

// New returns a Client targeting the given host. The host may be a bare
// host:port — a scheme is prepended if missing.
func New(host string) *Client {
	return &Client{
		host: normalizeHost(host),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewFromEnv returns a Client reading OLLAMA_HOST and falling back to
// DefaultHost. This matches Ollama's own resolution.
func NewFromEnv() *Client {
	host := os.Getenv(EnvHost)
	if host == "" {
		host = DefaultHost
	}
	return New(host)
}

// Host returns the resolved base URL.
func (c *Client) Host() string { return c.host }

// WithHTTPClient swaps the underlying http.Client; intended for tests.
func (c *Client) WithHTTPClient(h *http.Client) *Client {
	c.http = h
	return c
}

// Available probes /api/tags and reports whether Ollama responded.
// It uses a short timeout so a dead server fails fast.
func (c *Client) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ListModels returns the models known to the Ollama server.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama not available at %s: %w", c.host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama list returned %s", resp.Status)
	}
	var out TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// Delete removes a model by name from the Ollama server.
func (c *Client) Delete(ctx context.Context, name string) error {
	body, _ := json.Marshal(deleteRequest{Name: name})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.host+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama delete failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama delete %q: %s: %s", name, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// PullProgress is one progress event emitted during Pull.
type PullProgress struct {
	Status string
}

// Pull streams /api/pull and invokes onStatus for each status line. onStatus
// may be nil. Pull returns when the server closes the stream or signals
// error.
func (c *Client) Pull(ctx context.Context, name string, onStatus func(PullProgress)) error {
	// Pull can take a long time; use a per-request HTTP client without the
	// default short timeout.
	puller := &http.Client{Transport: c.http.Transport}

	body, _ := json.Marshal(pullRequest{Name: name, Stream: true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := puller.Do(req)
	if err != nil {
		return fmt.Errorf("ollama pull failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama pull %q: %s: %s", name, resp.Status, strings.TrimSpace(string(msg)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var s pullStatus
		if err := json.Unmarshal(line, &s); err != nil {
			continue
		}
		if s.Error != "" {
			return errors.New(s.Error)
		}
		if onStatus != nil {
			onStatus(PullProgress{Status: s.Status})
		}
	}
	return scanner.Err()
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return DefaultHost
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/")
}
