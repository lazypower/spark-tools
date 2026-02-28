// Package api implements the HuggingFace Hub REST API client.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/pkg/hfetch/auth"
)

var errRangeNotSupported = errors.New("range not supported")

// IsRangeNotSupported reports whether the error indicates the server
// does not support HTTP Range requests (returned 200 instead of 206).
func IsRangeNotSupported(err error) bool {
	return errors.Is(err, errRangeNotSupported)
}

const (
	defaultBaseURL     = "https://huggingface.co"
	defaultAPIBase     = "https://huggingface.co/api"
	defaultMaxRetries  = 3
	defaultConcurrency = 2
)

// Client is an HTTP client for the HuggingFace Hub API.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiBase     string
	token       string
	maxRetries  int
	concurrency int
	cacheDir    string // optional directory for API metadata caching
}

// Option configures the Client.
type Option func(*Client)

// WithToken sets an explicit auth token.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithBaseURL overrides the HuggingFace base URL (useful for testing).
func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(u, "/")
		c.apiBase = c.baseURL + "/api"
	}
}

// NewClient creates a new HuggingFace API client.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
				DisableCompression:    true, // Prevent Range+gzip conflicts with CDN redirects
			},
		},
		baseURL:     defaultBaseURL,
		apiBase:     defaultAPIBase,
		maxRetries:  defaultMaxRetries,
		concurrency: defaultConcurrency,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WhoAmI validates a token against the HuggingFace API and returns
// the authenticated user's information.
func (c *Client) WhoAmI(ctx context.Context) (*auth.UserInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.apiBase+"/whoami", nil)
	if err != nil {
		return nil, err
	}

	if c.token == "" {
		return nil, auth.ErrAuthRequired
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info auth.UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding whoami response: %w", err)
	}
	return &info, nil
}

// Search finds models on HuggingFace Hub.
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) ([]Model, error) {
	params := url.Values{}
	params.Set("search", query)
	if opts.Filter != "" {
		params.Set("filter", opts.Filter)
	}
	if opts.Sort != "" {
		params.Set("sort", opts.Sort)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	params.Set("limit", strconv.Itoa(limit))

	req, err := c.newRequest(ctx, http.MethodGet, c.apiBase+"/models?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var models []Model
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}
	return models, nil
}

// GetModel retrieves metadata for a specific model.
// Results are cached to HFETCH_CACHE_DIR/models/<org>--<model>/meta.json
// with a 24-hour TTL.
func (c *Client) GetModel(ctx context.Context, modelID string) (*Model, error) {
	// Check cache first.
	cachePath := c.cacheModelPath(modelID)
	if cached, ok := c.loadCache(cachePath); ok {
		var model Model
		if err := json.Unmarshal(cached, &model); err == nil {
			return &model, nil
		}
	}

	req, err := c.newRequest(ctx, http.MethodGet, c.apiBase+"/models/"+modelID, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading model response: %w", err)
	}

	var model Model
	if err := json.Unmarshal(body, &model); err != nil {
		return nil, fmt.Errorf("decoding model response: %w", err)
	}

	// Cache the response.
	c.saveCache(cachePath, body)

	return &model, nil
}

// ListFiles lists files in a model repository.
func (c *Client) ListFiles(ctx context.Context, modelID string) ([]ModelFile, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.apiBase+"/models/"+modelID+"/tree/main", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var files []ModelFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("decoding file list response: %w", err)
	}
	return files, nil
}

// HeadFile performs a HEAD request to get file size and hash without downloading.
// It uses RoundTrip directly (no redirect following) because HuggingFace
// returns X-Linked-Etag (the LFS SHA256) and X-Linked-Size on the initial
// 302 response; following the redirect to the CDN loses these headers.
func (c *Client) HeadFile(ctx context.Context, modelID, filename string) (size int64, sha256 string, err error) {
	u := fmt.Sprintf("%s/%s/resolve/main/%s", c.baseURL, modelID, filename)
	req, err := c.newRequest(ctx, http.MethodHead, u, nil)
	if err != nil {
		return 0, "", err
	}

	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return 0, "", fmt.Errorf("HEAD request failed: %w", err)
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return 0, "", auth.ErrAuthInvalid
	case http.StatusForbidden:
		return 0, "", auth.ErrGatedModel
	case http.StatusNotFound:
		return 0, "", fmt.Errorf("not found: %s", req.URL.Path)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return 0, "", fmt.Errorf("HEAD request HTTP %d", resp.StatusCode)
	}

	// X-Linked-Etag is the LFS SHA256, only present on the HF response.
	sha256 = resp.Header.Get("X-Linked-Etag")
	if sha256 == "" {
		sha256 = resp.Header.Get("ETag")
	}
	sha256 = strings.Trim(sha256, "\"")

	// For redirect responses, Content-Length is the redirect body size,
	// NOT the actual file size. Use X-Linked-Size or follow the redirect.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if v := resp.Header.Get("X-Linked-Size"); v != "" {
			size, _ = strconv.ParseInt(v, 10, 64)
		}
		if size <= 0 {
			if loc := resp.Header.Get("Location"); loc != "" {
				cdnReq, _ := http.NewRequestWithContext(ctx, http.MethodHead, loc, nil)
				if cdnReq != nil {
					cdnResp, err2 := c.httpClient.Do(cdnReq)
					if err2 == nil {
						size = cdnResp.ContentLength
						cdnResp.Body.Close()
					}
				}
			}
		}
	} else {
		size = resp.ContentLength
	}

	return size, sha256, nil
}

// FetchFileRange fetches a specific byte range of a file. Used for remote
// GGUF header parsing (~4KB) without downloading the full file.
func (c *Client) FetchFileRange(ctx context.Context, modelID, filename string, start, end int64) ([]byte, error) {
	u := fmt.Sprintf("%s/%s/resolve/main/%s", c.baseURL, modelID, filename)
	req, err := c.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading range response: %w", err)
	}
	return data, nil
}

// DownloadFile opens a download stream for a file. The caller must close
// the returned ReadCloser. Supports Range requests via offset.
func (c *Client) DownloadFile(ctx context.Context, modelID, filename string, offset int64) (io.ReadCloser, int64, error) {
	u := fmt.Sprintf("%s/%s/resolve/main/%s", c.baseURL, modelID, filename)
	req, err := c.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}

	// If we requested a Range but got 200 (not 206), the server
	// doesn't support Range requests (e.g. HuggingFace Xet storage).
	if offset > 0 && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, 0, errRangeNotSupported
	}

	return resp.Body, resp.ContentLength, nil
}

// newRequest creates an HTTP request with auth headers.
func (c *Client) newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("User-Agent", "hfetch/0.1")
	return req, nil
}

// do executes a request with retry logic for rate limits and server errors.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := range c.maxRetries + 1 {
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed: %w", err)
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			resp.Body.Close()
			return nil, auth.ErrAuthInvalid
		case resp.StatusCode == http.StatusForbidden:
			resp.Body.Close()
			return nil, auth.ErrGatedModel
		case resp.StatusCode == http.StatusTooManyRequests:
			resp.Body.Close()
			if attempt < c.maxRetries {
				c.backoff(attempt, resp.Header.Get("Retry-After"))
				continue
			}
			return nil, fmt.Errorf("rate limited after %d retries", c.maxRetries)
		case resp.StatusCode >= 500:
			resp.Body.Close()
			if attempt < c.maxRetries {
				c.backoff(attempt, "")
				continue
			}
			return nil, fmt.Errorf("server error %d after %d retries", resp.StatusCode, c.maxRetries)
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			return nil, fmt.Errorf("not found: %s", req.URL.Path)
		case resp.StatusCode >= 400:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		return resp, nil
	}

	return resp, err
}

// backoff waits with exponential backoff + jitter.
func (c *Client) backoff(attempt int, retryAfter string) {
	wait := time.Duration(math.Pow(2, float64(attempt))) * time.Second

	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil {
			wait = time.Duration(secs) * time.Second
		}
	}

	// Add jitter: ±25%
	jitter := time.Duration(float64(wait) * (0.5*rand.Float64() - 0.25))
	time.Sleep(wait + jitter)
}
