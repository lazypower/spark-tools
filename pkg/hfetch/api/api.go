// Package api is a compatibility wrapper over internal/hub. The HuggingFace Hub
// REST API client (search, model metadata, tree listing with pagination, HEAD,
// range fetch, download, retry/backoff, and the model-metadata cache) moved to
// internal/hub during the /internal extraction; this thin alias keeps existing
// importers (cmd/hfetch, cmd/llm-serve, pkg/hfetch, pkg/hfetch/source,
// pkg/hfetch/fileset, pkg/llmserve/artifact, pkg/seam) compiling unchanged until
// they migrate. Type aliases carry the Client methods over; the constructor,
// options, and IsRangeNotSupported predicate delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/hub.
package api

import (
	"net/http"

	"github.com/lazypower/spark-tools/internal/hub"
)

// CacheTTL is how long a cached model-metadata entry stays fresh.
const CacheTTL = hub.CacheTTL

// Type aliases — carry the Client methods (WhoAmI/Search/GetModel/ListFiles/
// HeadFile/FetchFileRange/DownloadFile) over and keep request/response values
// flowing across the boundary as the same type.
type (
	Client        = hub.Client
	Option        = hub.Option
	Model         = hub.Model
	Sibling       = hub.Sibling
	ModelFile     = hub.ModelFile
	LFS           = hub.LFS
	SearchOptions = hub.SearchOptions
)

// NewClient constructs a Hub client.
func NewClient(opts ...Option) *Client { return hub.NewClient(opts...) }

// WithToken sets the bearer token sent with each request.
func WithToken(token string) Option { return hub.WithToken(token) }

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option { return hub.WithHTTPClient(hc) }

// WithBaseURL overrides the HuggingFace base URL (useful for testing).
func WithBaseURL(u string) Option { return hub.WithBaseURL(u) }

// WithCacheDir enables on-disk caching of model metadata under dir.
func WithCacheDir(dir string) Option { return hub.WithCacheDir(dir) }

// IsRangeNotSupported reports whether err signals the server rejected a Range
// request.
func IsRangeNotSupported(err error) bool { return hub.IsRangeNotSupported(err) }
