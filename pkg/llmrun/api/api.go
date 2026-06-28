// Package api is a compatibility wrapper over internal/openaiapi. The HTTP client
// for llama-server's OpenAI-compatible /v1/* endpoints moved to internal/openaiapi
// during the /internal extraction (named to disambiguate from hfetch's api); this
// thin alias keeps existing importers (cmd/llm-run, cmd/llm-chat, internal/tui)
// compiling unchanged until they migrate. Type aliases carry the Client methods
// over; the constructor and options delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/openaiapi.
package api

import (
	"net/http"

	oai "github.com/lazypower/spark-tools/internal/openaiapi"
)

// Type aliases — carry the Client methods (Health/ListModels/ChatCompletion/
// ChatCompletionStream/Completion) over and keep request/response values flowing
// across the boundary as the same type.
type (
	Client                 = oai.Client
	Option                 = oai.Option
	ChatCompletionRequest  = oai.ChatCompletionRequest
	ResponseFormat         = oai.ResponseFormat
	CompletionRequest      = oai.CompletionRequest
	Message                = oai.Message
	ChatCompletionResponse = oai.ChatCompletionResponse
	Choice                 = oai.Choice
	CompletionResponse     = oai.CompletionResponse
	CompletionChoice       = oai.CompletionChoice
	Usage                  = oai.Usage
	ModelInfo              = oai.ModelInfo
	ModelListResponse      = oai.ModelListResponse
	HealthResponse         = oai.HealthResponse
	StreamDelta            = oai.StreamDelta
)

// NewClient returns a client targeting baseURL.
func NewClient(baseURL string, opts ...Option) *Client { return oai.NewClient(baseURL, opts...) }

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option { return oai.WithHTTPClient(hc) }

// WithAPIKey sets the bearer API key sent with each request.
func WithAPIKey(key string) Option { return oai.WithAPIKey(key) }
