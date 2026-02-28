package job

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeRequest_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// First chunk (delta)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		// Second chunk with timings
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}],\"timings\":{\"prompt_n\":10,\"prompt_ms\":5.2,\"predicted_n\":50,\"predicted_ms\":1000.0}}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	result := probeRequest(context.Background(), server.URL, "Hello", 100, 0.0)
	if result.Err != nil {
		t.Fatalf("probeRequest: %v", result.Err)
	}

	// TTFT should be > 0
	if result.Sample.TTFTMs <= 0 {
		t.Errorf("TTFT should be > 0, got %f", result.Sample.TTFTMs)
	}

	// End-to-end should be >= TTFT
	if result.Sample.EndToEndMs < result.Sample.TTFTMs {
		t.Errorf("E2E (%f) should be >= TTFT (%f)", result.Sample.EndToEndMs, result.Sample.TTFTMs)
	}

	// Timings from last chunk
	if result.Timings == nil {
		t.Fatal("expected timings")
	}
	if result.Timings.PromptN != 10 {
		t.Errorf("prompt_n: got %d, want 10", result.Timings.PromptN)
	}
	if result.Timings.PredictedN != 50 {
		t.Errorf("predicted_n: got %d, want 50", result.Timings.PredictedN)
	}
}

func TestProbeRequest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := probeRequest(context.Background(), server.URL, "Hello", 100, 0.0)
	if result.Err == nil {
		t.Error("expected error for server error")
	}
}

func TestExtractTimingsFromSSE(t *testing.T) {
	data := []byte(`{"choices":[{"delta":{"content":"x"}}],"timings":{"prompt_n":42,"prompt_ms":22.8,"predicted_n":512,"predicted_ms":10500.5}}`)

	timings := extractTimingsFromSSE(data)
	if timings == nil {
		t.Fatal("expected timings")
	}
	if timings.PromptN != 42 {
		t.Errorf("prompt_n: got %d", timings.PromptN)
	}
	if timings.PredictedN != 512 {
		t.Errorf("predicted_n: got %d", timings.PredictedN)
	}
}

func TestExtractTimingsFromSSE_UsageFallback(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":50,"total_tokens":60}}`)

	timings := extractTimingsFromSSE(data)
	if timings == nil {
		t.Fatal("expected timings from usage fallback")
	}
	if timings.PromptN != 10 {
		t.Errorf("prompt_n: got %d", timings.PromptN)
	}
}

func TestCooldown(t *testing.T) {
	ctx := context.Background()
	err := Cooldown(ctx, 0)
	if err != nil {
		t.Errorf("cooldown(0): %v", err)
	}
}

func TestCooldown_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Cooldown(ctx, 60)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
