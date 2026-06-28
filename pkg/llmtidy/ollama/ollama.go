// Package ollama is a compatibility wrapper over internal/ollama. The minimal
// Ollama REST client moved to internal/ollama during the /internal extraction;
// this thin alias keeps existing importers (pkg/llmtidy/{inventory,reconcile,
// llmtidy}) compiling unchanged until they migrate. The Client type alias carries
// its Host/Available/ListModels/Delete/Pull/WithHTTPClient methods over; the
// constructors and consts delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/ollama.
package ollama

import (
	iollama "github.com/lazypower/spark-tools/internal/ollama"
)

// DefaultHost is the Ollama server address used when OLLAMA_HOST is unset.
const DefaultHost = iollama.DefaultHost

// EnvHost is the environment variable Ollama itself reads for its address.
const EnvHost = iollama.EnvHost

// Type aliases — carry the Client methods over and keep values flowing across
// the boundary as the same type.
type (
	Client       = iollama.Client
	Model        = iollama.Model
	TagsResponse = iollama.TagsResponse
	PullProgress = iollama.PullProgress
)

// New returns a Client targeting the given host.
func New(host string) *Client { return iollama.New(host) }

// NewFromEnv returns a Client reading OLLAMA_HOST and falling back to DefaultHost.
func NewFromEnv() *Client { return iollama.NewFromEnv() }
