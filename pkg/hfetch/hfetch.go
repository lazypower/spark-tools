// Package hfetch provides a unified client for HuggingFace model
// discovery, download, and local registry management.
//
// This is the primary import for downstream tools (llm-run, llm-bench).
// It wraps the sub-packages (api, auth, config, download, gguf, registry)
// into a single cohesive API.
package hfetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/download"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// Re-export key types from sub-packages for convenience.
type (
	Model        = api.Model
	ModelFile    = api.ModelFile
	GGUFMetadata = gguf.GGUFMetadata
	LocalModel   = registry.LocalModel
	LocalFile    = registry.LocalFile
	FileInfo     = gguf.FileInfo
	FitResult    = gguf.FitResult
)

// SearchOptions configures a model search.
type SearchOptions = api.SearchOptions

// PullOptions configures a download.
type PullOptions struct {
	OutputDir    string
	Streams      int
	MaxBandwidth int64 // bytes per second, 0 = unlimited
	OnProgress   download.ProgressFunc
}

// ProgressEvent is re-exported from download.
type ProgressEvent = download.ProgressEvent

// ProgressFunc is re-exported from download.
type ProgressFunc = download.ProgressFunc

// Client is the main entry point for the hfetch library.
type Client struct {
	api      *api.Client
	registry *registry.Registry
	dirs     config.DirConfig
}

// Option configures the Client.
type Option func(*clientConfig)

type clientConfig struct {
	token      string
	httpClient *http.Client
	baseURL    string
	cacheDir   string
}

// WithToken provides an explicit token override (priority 0 in resolution order).
func WithToken(token string) Option {
	return func(c *clientConfig) { c.token = token }
}

// WithCacheDir overrides the cache directory.
func WithCacheDir(dir string) Option {
	return func(c *clientConfig) { c.cacheDir = dir }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) { c.httpClient = hc }
}

// WithBaseURL overrides the HuggingFace base URL (for testing).
func WithBaseURL(u string) Option {
	return func(c *clientConfig) { c.baseURL = u }
}

// NewClient creates a new HuggingFace client with the given options.
func NewClient(opts ...Option) (*Client, error) {
	cfg := &clientConfig{}
	for _, o := range opts {
		o(cfg)
	}

	// Resolve token.
	tok := config.ResolveToken(cfg.token)

	// Build API client options.
	var apiOpts []api.Option
	apiOpts = append(apiOpts, api.WithToken(tok.Token))
	if cfg.httpClient != nil {
		apiOpts = append(apiOpts, api.WithHTTPClient(cfg.httpClient))
	}
	if cfg.baseURL != "" {
		apiOpts = append(apiOpts, api.WithBaseURL(cfg.baseURL))
	}

	dirs := config.Dirs()
	if cfg.cacheDir != "" {
		dirs.Cache = cfg.cacheDir
	}

	apiOpts = append(apiOpts, api.WithCacheDir(dirs.Cache))

	reg := registry.New(dirs.Data)
	if err := reg.Load(); err != nil {
		return nil, fmt.Errorf("loading registry: %w", err)
	}

	return &Client{
		api:      api.NewClient(apiOpts...),
		registry: reg,
		dirs:     dirs,
	}, nil
}

// Search finds models on HuggingFace Hub.
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) ([]Model, error) {
	return c.api.Search(ctx, query, opts)
}

// GetModel retrieves metadata for a specific model.
func (c *Client) GetModel(ctx context.Context, modelID string) (*Model, error) {
	return c.api.GetModel(ctx, modelID)
}

// ListFiles lists files in a model repository.
func (c *Client) ListFiles(ctx context.Context, modelID string) ([]ModelFile, error) {
	return c.api.ListFiles(ctx, modelID)
}

// FetchGGUFMetadata fetches and parses the GGUF header of a remote file
// using an HTTP Range request (~8KB). No full download required.
func (c *Client) FetchGGUFMetadata(ctx context.Context, modelID, filename string) (*GGUFMetadata, error) {
	// Fetch first 8KB — enough for most GGUF headers.
	data, err := c.api.FetchFileRange(ctx, modelID, filename, 0, 8191)
	if err != nil {
		return nil, fmt.Errorf("fetching GGUF header: %w", err)
	}
	return gguf.Parse(bytes.NewReader(data))
}

// Pull downloads a model file and registers it in the local registry.
func (c *Client) Pull(ctx context.Context, modelID, filename string, opts PullOptions) (*LocalFile, error) {
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = c.registry.ModelDir(modelID)
	}

	src := &apiFileSource{
		client:  c.api,
		modelID: modelID,
		file:    filename,
	}

	streams := opts.Streams
	if streams <= 0 {
		streams = 4
	}

	finalPath, err := download.Download(ctx, src, filename, download.Options{
		OutputDir:    outputDir,
		Streams:      streams,
		MaxBandwidth: opts.MaxBandwidth,
		OnProgress:   opts.OnProgress,
	})
	if err != nil {
		return nil, err
	}

	// Get file size for registry.
	size, _, _ := c.api.HeadFile(ctx, modelID, filename)
	quant := gguf.ParseQuantFromFilename(filename)

	lf := registry.LocalFile{
		Filename:     filename,
		Size:         size,
		Quantization: quant,
		LocalPath:    finalPath,
		Complete:     true,
		DownloadedAt: time.Now(),
	}
	c.registry.AddFile(modelID, lf)
	if err := c.registry.Save(); err != nil {
		return nil, fmt.Errorf("saving registry: %w", err)
	}

	return &lf, nil
}

// Registry returns access to locally downloaded models.
func (c *Client) Registry() *registry.Registry {
	return c.registry
}

// apiFileSource adapts the API client to the download.FileSource interface.
type apiFileSource struct {
	client  *api.Client
	modelID string
	file    string
}

func (s *apiFileSource) Head(ctx context.Context) (int64, string, error) {
	return s.client.HeadFile(ctx, s.modelID, s.file)
}

func (s *apiFileSource) Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error) {
	return s.client.DownloadFile(ctx, s.modelID, s.file, offset)
}
