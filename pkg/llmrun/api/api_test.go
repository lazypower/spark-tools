package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	oai "github.com/lazypower/spark-tools/internal/openaiapi"
)

// The behavior suite (health, list, chat/completion, streaming, api-key, HTTP
// errors) lives in internal/openaiapi; this locks the compat surface (alias
// identity, method ride-along, delegated constructor + options).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ *oai.Client = (*Client)(nil)
	var _ oai.ChatCompletionRequest = ChatCompletionRequest{}
	var _ oai.Message = Message{}
	var _ oai.Usage = Usage{}
	var _ oai.StreamDelta = StreamDelta{}
	var _ oai.HealthResponse = HealthResponse{}
}

func TestWrapper_ConstructorAndHealthDelegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("k"))
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h == nil {
		t.Fatal("expected a health response through the wrapper client")
	}
}
