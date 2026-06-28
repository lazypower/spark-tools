package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPProber_Health(t *testing.T) {
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && healthy {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := NewHTTPProber()
	ok, err := p.Health(context.Background(), srv.URL)
	if err != nil || !ok {
		t.Errorf("healthy endpoint: ok=%v err=%v", ok, err)
	}
	healthy = false
	ok, _ = p.Health(context.Background(), srv.URL)
	if ok {
		t.Error("503 must report not-healthy")
	}
}

func TestHTTPProber_Health_Unreachable(t *testing.T) {
	p := NewHTTPProber()
	// An unreachable endpoint is not-healthy (error surfaced; caller fails closed).
	ok, err := p.Health(context.Background(), "http://127.0.0.1:0")
	if ok || err == nil {
		t.Errorf("unreachable endpoint must be not-healthy with error, got ok=%v err=%v", ok, err)
	}
}

func TestHTTPProber_Warmup_ServesRequestedModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"qwen-36b-fp4"`) {
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"!"}}]}`)
	}))
	defer srv.Close()

	p := NewHTTPProber()
	ok, err := p.Warmup(context.Background(), srv.URL, "qwen-36b-fp4")
	if err != nil || !ok {
		t.Errorf("warmup of served model: ok=%v err=%v", ok, err)
	}

	// Wrong served name ⇒ the endpoint 404s ⇒ not serving THIS model.
	ok, err = p.Warmup(context.Background(), srv.URL, "some-other-model")
	if ok || err == nil {
		t.Errorf("warmup of an unserved model must fail, got ok=%v err=%v", ok, err)
	}
}

func TestHTTPProber_Warmup_EmptyContent_Fails(t *testing.T) {
	// codex Mode-B P1: a 200 with an empty completion is NOT evidence of serving;
	// the predicate requires a non-empty generation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":""}}]}`)
	}))
	defer srv.Close()
	p := NewHTTPProber()
	if ok, err := p.Warmup(context.Background(), srv.URL, "m"); ok || err == nil {
		t.Errorf("an empty completion must fail warmup, got ok=%v err=%v", ok, err)
	}
}

func TestHTTPProber_Warmup_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 but an embedded error object ⇒ not a real generation.
		io.WriteString(w, `{"error":{"message":"engine dead"}}`)
	}))
	defer srv.Close()
	p := NewHTTPProber()
	if ok, err := p.Warmup(context.Background(), srv.URL, "m"); ok || err == nil {
		t.Errorf("a 200-with-error-body must fail warmup, got ok=%v err=%v", ok, err)
	}
}
