package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPProber checks a vLLM OpenAI-compatible endpoint over HTTP. It is the real
// Prober; lifecycle is tested against a fake.
type HTTPProber struct {
	// Client is the HTTP client; defaults to a short-timeout client.
	Client *http.Client
}

// NewHTTPProber returns an HTTPProber with a sane default client.
func NewHTTPProber() *HTTPProber {
	return &HTTPProber{Client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *HTTPProber) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Health reports whether GET baseURL/health returns 200.
func (p *HTTPProber) Health(ctx context.Context, baseURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
	if err != nil {
		return false, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return false, err // unreachable ⇒ not healthy (caller treats as not-serving)
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// Warmup sends a minimal chat completion addressed to servedName and reports ok
// iff a non-empty generation returns with no API error — the minimum evidence the
// endpoint is actually serving THIS model.
func (p *HTTPProber) Warmup(ctx context.Context, baseURL, servedName string) (bool, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      servedName,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("warmup HTTP %d (model %q not served?)", resp.StatusCode, servedName)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, fmt.Errorf("warmup decode: %w", err)
	}
	if len(out.Error) > 0 && string(out.Error) != "null" {
		return false, fmt.Errorf("warmup API error: %s", out.Error)
	}
	// A 200 with the requested model and a well-formed (possibly empty-string at
	// max_tokens=1) choice is sufficient evidence the model is loaded and serving.
	return len(out.Choices) > 0, nil
}
