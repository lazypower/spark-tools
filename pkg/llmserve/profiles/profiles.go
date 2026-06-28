// Package profiles is a compatibility wrapper over internal/serveprofiles. The
// serving architecture-profile registry, quant-flag table, and negative-compat
// rules moved to internal/serveprofiles during the /internal extraction (named to
// disambiguate from llm-run's profiles); this thin alias keeps existing importers
// (pkg/llmserve/contract, pkg/llmserve) compiling unchanged until they migrate.
// Type aliases carry the ArchProfile.Supports method over; the lookup/quant funcs
// and the CompatRules/QuantFlags data tables delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/serveprofiles.
package profiles

import (
	"github.com/lazypower/spark-tools/internal/serving"
	sp "github.com/lazypower/spark-tools/internal/serveprofiles"
)

// Type aliases — carry methods (ArchProfile.Supports) over and keep values
// flowing across the boundary as the same type.
type (
	ClaimStatus   = sp.ClaimStatus
	Claim         = sp.Claim
	ArchProfile   = sp.ArchProfile
	CompatRequest = sp.CompatRequest
	CompatRule    = sp.CompatRule
)

// Claim status enum.
const (
	StatusAsserted = sp.StatusAsserted
	StatusProven   = sp.StatusProven
	StatusDrifted  = sp.StatusDrifted
)

// Data tables — re-exported by identity (slices/maps are reference types, so
// readers see the same backing store as the authority).
var (
	// QuantFlags maps a quant method to the vLLM flags it requires.
	QuantFlags = sp.QuantFlags
	// CompatRules is the ordered list of negative-compatibility rules.
	CompatRules = sp.CompatRules
)

// QuantFlagsFor returns the vLLM flags required for a quant method.
func QuantFlagsFor(q serving.QuantMethod) (flags []string, ok bool) { return sp.QuantFlagsFor(q) }

// BuiltinProfiles returns the built-in architecture profiles.
func BuiltinProfiles() []ArchProfile { return sp.BuiltinProfiles() }

// Lookup returns the architecture profile for an arch, if known.
func Lookup(arch string) (ArchProfile, bool) { return sp.Lookup(arch) }
