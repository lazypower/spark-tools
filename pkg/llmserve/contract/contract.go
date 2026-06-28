// Package contract is a compatibility wrapper over internal/servecontract. The
// serve-request contract engine (resolve requested capabilities into validated,
// ordered vLLM flags; reject incompatible combinations; stamp the contract key)
// moved to internal/servecontract during the /internal extraction; this thin
// alias keeps existing importers (pkg/llmserve, pkg/llmserve/emit, cmd/llm-serve,
// pkg/seam) compiling unchanged until they migrate. The type aliases carry the
// RejectionError.Error method over; the funcs delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/servecontract.
package contract

import (
	sc "github.com/lazypower/spark-tools/internal/servecontract"
	"github.com/lazypower/spark-tools/internal/serving"
)

// Type aliases — carry the RejectionError.Error method over and keep values
// flowing across the boundary as the same type.
type (
	Request        = sc.Request
	Resolved       = sc.Resolved
	RejectionError = sc.RejectionError
)

// AsRejection extracts a *RejectionError from an error chain, if present.
func AsRejection(err error) (*RejectionError, bool) { return sc.AsRejection(err) }

// Resolve validates a request against the artifact facts and the arch profiles.
func Resolve(req Request, facts serving.ArtifactFacts) (*Resolved, error) {
	return sc.Resolve(req, facts)
}

// CanonicalCapabilities returns the requested capabilities in canonical order.
func CanonicalCapabilities(caps []serving.Capability) []serving.Capability {
	return sc.CanonicalCapabilities(caps)
}
