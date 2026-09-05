package openaiapi

import (
	"encoding/json"
	"testing"
)

// Servers disagree on the key. vLLM 0.28 returns "reasoning"; others use
// "reasoning_content". Parsing only one silently discards a thinking model's
// output against half the ecosystem.
func TestMessage_ReasoningFieldVariants(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"vllm 0.28 style", `{"role":"assistant","content":"4","reasoning":"2+2 is 4"}`, "2+2 is 4"},
		{"reasoning_content style", `{"role":"assistant","content":"4","reasoning_content":"2+2 is 4"}`, "2+2 is 4"},
		{"neither", `{"role":"assistant","content":"4"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m Message
			if err := json.Unmarshal([]byte(c.body), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := m.ReasoningText(); got != c.want {
				t.Errorf("ReasoningText() = %q, want %q", got, c.want)
			}
			if m.Content != "4" {
				t.Errorf("content must still parse, got %q", m.Content)
			}
		})
	}
}

// The field must not be emitted when empty, so requests stay unchanged.
func TestMessage_OmitsEmptyReasoning(t *testing.T) {
	b, err := json.Marshal(Message{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); got != `{"role":"user","content":"hi"}` {
		t.Errorf("outgoing message changed shape: %s", got)
	}
}
