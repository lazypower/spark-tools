package serveprofiles

import (
	"testing"

	"github.com/lazypower/spark-tools/internal/serving"
)

func TestLookup_Qwen3Dense_Exists(t *testing.T) {
	p, ok := Lookup("Qwen3ForCausalLM")
	if !ok {
		t.Fatal("plain dense Qwen3 must have a serving profile; without it llm-serve rejects a large swath of ordinary text models")
	}
	if p.Arch != "Qwen3ForCausalLM" {
		t.Errorf("arch = %q", p.Arch)
	}
}

// The parser choice is the whole reason this is its own profile rather than an
// alt of the MoE entry. qwen3_coder parses Qwen3-Coder's
// <function=...><parameter=...> XML; this arch's template emits Hermes-shaped
// <tool_call>{"name":...}</tool_call>, so the MoE parser cannot read its output.
func TestLookup_Qwen3Dense_UsesHermesNotQwen3Coder(t *testing.T) {
	p, _ := Lookup("Qwen3ForCausalLM")
	if p.ToolCallParser != "hermes" {
		t.Errorf("tool parser = %q, want hermes", p.ToolCallParser)
	}
	if p.ReasoningParser != "qwen3" {
		t.Errorf("reasoning parser = %q, want qwen3", p.ReasoningParser)
	}

	// And the MoE entry must be untouched by this addition.
	moe, ok := Lookup("Qwen3MoeForCausalLM")
	if !ok {
		t.Fatal("Qwen3MoeForCausalLM profile missing")
	}
	if moe.ToolCallParser != "qwen3_coder" {
		t.Errorf("MoE tool parser changed to %q; it must stay qwen3_coder", moe.ToolCallParser)
	}
}

// The dense text line has no vision or image keys in its config at all, so a
// vision request against it must be refused rather than silently attempted.
func TestLookup_Qwen3Dense_Claims(t *testing.T) {
	p, _ := Lookup("Qwen3ForCausalLM")

	want := map[serving.Capability]bool{
		serving.GuidedDecoding: true,
		serving.Thinking:       true,
		serving.ToolCalling:    true,
		serving.Vision:         false,
	}
	got := make(map[serving.Capability]bool, len(p.Claims))
	for _, c := range p.Claims {
		got[c.Capability] = c.Supported
		if c.Status != StatusAsserted {
			t.Errorf("%s: status = %q, want asserted (v1 claims are hypotheses)", c.Capability, c.Status)
		}
		if c.Provenance != qwen3CausalProvenance {
			t.Errorf("%s: provenance must record that the claim came from the artifact template, got %q", c.Capability, c.Provenance)
		}
	}
	for cap, wantSupported := range want {
		if got[cap] != wantSupported {
			t.Errorf("claim %s = %v, want %v", cap, got[cap], wantSupported)
		}
	}
}

// These claims were not authored against the repo-wide GB10 seed, and stamping
// them that way would assert a provenance they do not have.
func TestLookup_Qwen3Dense_CarriesOwnAuthoredEnvironment(t *testing.T) {
	p, _ := Lookup("Qwen3ForCausalLM")
	if p.AuthoredAgainst == seededFingerprint {
		t.Error("the dense Qwen3 entry must not inherit the GB10 seed it was not authored against")
	}
	if p.AuthoredAgainst != qwen3CausalFingerprint {
		t.Errorf("authored against %+v, want %+v", p.AuthoredAgainst, qwen3CausalFingerprint)
	}
}

// Stamping must still fill in every entry that carries no anchor of its own.
func TestBuiltinProfiles_AllStamped(t *testing.T) {
	for _, p := range BuiltinProfiles() {
		if p.AuthoredAgainst.Zero() {
			t.Errorf("profile %q left unstamped", p.Arch)
		}
	}
	moe, _ := Lookup("Qwen3MoeForCausalLM")
	if moe.AuthoredAgainst != seededFingerprint {
		t.Error("an entry with no explicit anchor must still inherit the repo-wide seed")
	}
}
