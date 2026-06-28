package inventory

import "testing"

func TestModelBackendString(t *testing.T) {
	cases := map[ModelBackend]string{
		BackendOllama:  "ollama",
		BackendGGUF:    "gguf",
		BackendUnknown: "unknown",
	}
	for b, want := range cases {
		if got := b.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", b, got, want)
		}
	}
}

func TestParseBackend(t *testing.T) {
	if b, err := ParseBackend("ollama"); err != nil || b != BackendOllama {
		t.Errorf("ParseBackend(\"ollama\") = %v, %v", b, err)
	}
	if b, err := ParseBackend("gguf"); err != nil || b != BackendGGUF {
		t.Errorf("ParseBackend(\"gguf\") = %v, %v", b, err)
	}
	if _, err := ParseBackend("docker"); err == nil {
		t.Error("ParseBackend(\"docker\") should error")
	}
}
