package openaiapi

import "testing"

// The client appends its own versioned paths, so a base URL that already ends
// in /v1 must not double. Passing the conventional OpenAI base URL is the most
// natural thing a user can do and used to yield an opaque 404.
func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://127.0.0.1:11434/v1", "http://127.0.0.1:11434"},
		{"http://127.0.0.1:11434/v1/", "http://127.0.0.1:11434"},
		{"http://127.0.0.1:11434/", "http://127.0.0.1:11434"},
		{"http://127.0.0.1:11434", "http://127.0.0.1:11434"},
		{"  http://127.0.0.1:8000/v1  ", "http://127.0.0.1:8000"},
		// A path-mounted endpoint keeps its prefix: only the final /v1 goes.
		{"https://host/openai/v1", "https://host/openai"},
		{"https://host/openai", "https://host/openai"},
		// "v1" that is not its own trailing segment must be left alone.
		{"http://host/apiv1", "http://host/apiv1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeBaseURL(c.in); got != c.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNewClient_AcceptsEitherForm(t *testing.T) {
	withV1 := NewClient("http://127.0.0.1:11434/v1")
	without := NewClient("http://127.0.0.1:11434")
	if withV1.baseURL != without.baseURL {
		t.Errorf("both forms must resolve to the same base: %q vs %q", withV1.baseURL, without.baseURL)
	}
}
