// Package serving is a compatibility wrapper over internal/serving. The
// ubiquitous vocabulary of the vLLM serving contract (capabilities, quant
// methods, tokenizer families, artifact facts, and the contract key) moved to
// internal/serving during the /internal extraction; this thin alias keeps
// existing importers (pkg/llmserve/{artifact,contract,profiles,instance,plan},
// cmd/llm-serve, pkg/seam) compiling unchanged until they migrate. The type
// aliases carry the ContractKey.Canonical method over, and the re-exported
// consts/vars/funcs delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/serving.
package serving

import (
	iserving "github.com/lazypower/spark-tools/internal/serving"
)

// Type aliases — carry methods (e.g. ContractKey.Canonical) over unchanged and
// keep values flowing across the boundary as the same type.
type (
	Capability      = iserving.Capability
	QuantMethod     = iserving.QuantMethod
	TokenizerFamily = iserving.TokenizerFamily
	ArtifactFacts   = iserving.ArtifactFacts
	ContractKey     = iserving.ContractKey
)

// Capability enum.
const (
	GuidedDecoding = iserving.GuidedDecoding
	Thinking       = iserving.Thinking
	ToolCalling    = iserving.ToolCalling
	Vision         = iserving.Vision
)

// QuantMethod enum.
const (
	QuantNone              = iserving.QuantNone
	QuantNVFP4             = iserving.QuantNVFP4
	QuantGPTQ              = iserving.QuantGPTQ
	QuantCompressedTensors = iserving.QuantCompressedTensors
	QuantFP8               = iserving.QuantFP8
)

// TokenizerFamily enum.
const (
	TokenizerUnknown = iserving.TokenizerUnknown
	TokenizerQwen    = iserving.TokenizerQwen
	TokenizerMistral = iserving.TokenizerMistral
	TokenizerLlama   = iserving.TokenizerLlama
	TokenizerGeneric = iserving.TokenizerGeneric
)

// AllCapabilities is the v1 capability enum, in canonical order.
var AllCapabilities = iserving.AllCapabilities

// ModeLabel renders a capability set as a stable, order-independent string.
func ModeLabel(caps []Capability) string { return iserving.ModeLabel(caps) }
