// Package fingerprint is a compatibility wrapper over internal/fingerprint. The
// environment-fingerprint authority moved to internal/fingerprint during the
// /internal extraction; this thin alias keeps existing importers (the llmserve
// packages, cmd/llm-serve, pkg/seam) compiling unchanged until they migrate.
// The Fingerprint type alias carries its Zero()/Canonical() methods over.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/fingerprint.
package fingerprint

import ifp "github.com/lazypower/spark-tools/internal/fingerprint"

// Fingerprint identifies the (engine build × accelerator) environment a serving
// contract was proven/asserted against. Alias of internal/fingerprint.Fingerprint.
type Fingerprint = ifp.Fingerprint

// Drift reports the dimensions in which target diverges from the stamped
// fingerprint. Delegates to internal/fingerprint.
func Drift(target, stamped Fingerprint) []string { return ifp.Drift(target, stamped) }
