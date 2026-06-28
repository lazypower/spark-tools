package serveprofiles

import (
	"slices"

	"github.com/lazypower/spark-tools/internal/serving"
)

// CompatRequest is the slice of a serve request the compat rules examine: the
// requested capabilities plus the artifact facts and the resolved arch profile.
// It is a read-only view so a rule can never mutate the resolution.
type CompatRequest struct {
	Capabilities []serving.Capability
	Facts        serving.ArtifactFacts
	Profile      ArchProfile
}

func (r CompatRequest) wants(c serving.Capability) bool {
	return slices.Contains(r.Capabilities, c)
}

// CompatRule is a declarative negative-compatibility rule (§3, codex #4):
// first-class data, evaluated at resolution, that rejects a footgun combination
// with a clear, actionable error instead of letting it become a silent flag
// side-effect. Violated returns the human-facing reason when the rule fires.
type CompatRule struct {
	// Name is a stable identifier for the rule (used in errors and tests).
	Name string
	// Violated reports whether the request trips the rule, and if so the reason
	// shown to the operator (what broke and why).
	Violated func(CompatRequest) (bool, string)
	// Remedy is the actionable fix surfaced alongside the rejection.
	Remedy string
}

// CompatRules is the v1 request-validation rule set, evaluated at resolution;
// any violation rejects the request (no partial/footgun launch). It holds the
// three production failure classes the campaign learned the hard way plus the
// capability-requires-fact gate: a profile claim says an ARCH can do X, but
// whether THIS ARTIFACT can is an artifact fact, and requesting a capability the
// artifact can't actually serve must reject, not silently emit a server that
// lacks it.
var CompatRules = []CompatRule{
	// Vision requires a multimodal processor in the artifact. The arch profile may
	// claim vision (the arch supports it), but a text-only build of that arch ships
	// no processor — vLLM would then serve text while the caller believes it
	// requested vision. Reject rather than silently mis-serve.
	{
		Name: "vision-requires-processor",
		Violated: func(r CompatRequest) (bool, string) {
			if r.wants(serving.Vision) && !r.Facts.HasVision {
				return true, "vision was requested but the artifact ships no multimodal processor (a text-only build of this arch)"
			}
			return false, ""
		},
		Remedy: "serve a vision build of this model, or drop the vision capability",
	},
	// reasoning_parser ⊗ guided_decoding. A reasoning parser makes vLLM defer the
	// grammar to post-</think> content; guided decoding then never activates (a
	// silent no-op). Requesting both reliable structured output AND thinking in
	// one launch cannot be honored — pick one.
	{
		Name: "reasoning-parser-disables-guided-decoding",
		Violated: func(r CompatRequest) (bool, string) {
			if r.wants(serving.Thinking) && r.wants(serving.GuidedDecoding) {
				return true, "thinking enables a reasoning parser, which defers the grammar to post-</think> content; guided decoding then silently never activates"
			}
			return false, ""
		},
		Remedy: "request guided-decoding OR thinking, not both (drop thinking for reliable structured output)",
	},
	// qwen3_coder tool parser requires a Qwen tokenizer. The parser 500s on a
	// non-Qwen tokenizer (no tool-call tokens). Fires when tool-calling is
	// requested on an arch whose tool parser is tokenizer-gated and the artifact's
	// tokenizer family does not match.
	{
		Name: "tool-parser-requires-matching-tokenizer",
		Violated: func(r CompatRequest) (bool, string) {
			req := r.Profile.ToolParserRequiresTokenizer
			if r.wants(serving.ToolCalling) && req != "" && r.Facts.Tokenizer != req {
				return true, "the " + r.Profile.ToolCallParser + " tool parser requires a " + string(req) +
					" tokenizer but the artifact ships a " + tokenizerName(r.Facts.Tokenizer) + " tokenizer; it returns 500 on a mismatched tokenizer"
			}
			return false, ""
		},
		Remedy: "drop tool-calling for this model, or serve a model whose tokenizer matches the tool parser",
	},
	// tokenizer_mode=mistral ⊗ vision. --tokenizer-mode mistral (selected by a
	// Tekken tokenizer) crashes on the vision path of a Mistral3 model. Fires when
	// the artifact has both a Mistral tokenizer and a vision processor.
	{
		Name: "mistral-tokenizer-mode-breaks-vision",
		Violated: func(r CompatRequest) (bool, string) {
			if r.Facts.Tokenizer == serving.TokenizerMistral && r.Facts.HasVision {
				return true, "--tokenizer-mode mistral (selected by the Tekken tokenizer) crashes on the vision path of this model"
			}
			return false, ""
		},
		Remedy: "serve the model without the mistral tokenizer mode, or use a non-vision build",
	},
}

func tokenizerName(t serving.TokenizerFamily) string {
	if t == serving.TokenizerUnknown {
		return "unknown"
	}
	return string(t)
}
