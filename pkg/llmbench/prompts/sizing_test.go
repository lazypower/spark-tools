package prompts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		prompt   string
		minCount int
	}{
		{"Hello", 1},
		{"Hello, how are you doing today?", 5},
		{"", 1}, // minimum 1
	}

	for _, tt := range tests {
		result := EstimateTokens(tt.prompt)
		if result.TokenCount < tt.minCount {
			t.Errorf("EstimateTokens(%q): got %d, want >= %d", tt.prompt, result.TokenCount, tt.minCount)
		}
		if result.Source != "estimated" {
			t.Errorf("source: got %q, want %q", result.Source, "estimated")
		}
	}
}

func TestTokenize_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenize" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string][]int{
			"tokens": {1, 2, 3, 4, 5},
		})
	}))
	defer server.Close()

	result, err := Tokenize(context.Background(), server.URL, "Hello world")
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if result.TokenCount != 5 {
		t.Errorf("token count: got %d, want 5", result.TokenCount)
	}
	if result.Source != "tokenized" {
		t.Errorf("source: got %q, want %q", result.Source, "tokenized")
	}
}

func TestTokenize_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := Tokenize(context.Background(), server.URL, "Hello")
	if err == nil {
		t.Error("expected error for server error")
	}
}
