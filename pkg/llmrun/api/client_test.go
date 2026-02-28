package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected /health, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(HealthResponse{
			Status:          "ok",
			SlotsIdle:       3,
			SlotsProcessing: 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	health, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" {
		t.Errorf("expected status ok, got %s", health.Status)
	}
	if health.SlotsIdle != 3 {
		t.Errorf("expected 3 idle slots, got %d", health.SlotsIdle)
	}
}

func TestHealth_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	health, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "error" {
		t.Errorf("expected status error, got %s", health.Status)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected /v1/models, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ModelListResponse{
			Object: "list",
			Data: []ModelInfo{
				{ID: "test-model", Object: "model", OwnedBy: "local"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Data) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models.Data))
	}
	if models.Data[0].ID != "test-model" {
		t.Errorf("expected test-model, got %s", models.Data[0].ID)
	}
}

func TestChatCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ChatCompletionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			t.Error("expected stream=false for non-streaming request")
		}

		json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:    "chatcmpl-1",
			Model: "test-model",
			Choices: []Choice{
				{
					Index:        0,
					Message:      &Message{Role: "assistant", Content: "Hello!"},
					FinishReason: "stop",
				},
			},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.ChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected Hello!, got %s", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestChatCompletionStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{"Hello", " ", "world", "!"}
		for _, chunk := range chunks {
			delta := StreamDelta{
				ID:    "chatcmpl-1",
				Model: "test-model",
				Choices: []Choice{
					{Index: 0, Delta: &Message{Role: "", Content: chunk}},
				},
			}
			data, _ := json.Marshal(delta)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		// Final chunk with finish_reason and usage (per OpenAI SSE spec).
		finalDelta := StreamDelta{
			ID:    "chatcmpl-1",
			Model: "test-model",
			Choices: []Choice{
				{Index: 0, Delta: &Message{}, FinishReason: "stop"},
			},
			Usage: &Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12},
		}
		data, _ := json.Marshal(finalDelta)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	var collected strings.Builder
	usage, err := c.ChatCompletionStream(context.Background(), ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}, func(delta StreamDelta) {
		if len(delta.Choices) > 0 && delta.Choices[0].Delta != nil {
			collected.WriteString(delta.Choices[0].Delta.Content)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if collected.String() != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", collected.String())
	}
	if usage.PromptTokens != 8 {
		t.Errorf("expected 8 prompt tokens, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 4 {
		t.Errorf("expected 4 completion tokens, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 12 {
		t.Errorf("expected 12 total tokens, got %d", usage.TotalTokens)
	}
}

func TestCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("expected /v1/completions, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(CompletionResponse{
			ID:    "cmpl-1",
			Model: "test-model",
			Choices: []CompletionChoice{
				{Index: 0, Text: "completed text", FinishReason: "stop"},
			},
			Usage: Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Completion(context.Background(), CompletionRequest{
		Prompt: "Once upon a time",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Text != "completed text" {
		t.Errorf("expected 'completed text', got %q", resp.Choices[0].Text)
	}
}

func TestAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %q", auth)
		}
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("test-key"))
	_, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestChatCompletion_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request body"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.ChatCompletion(context.Background(), ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected HTTP 400 in error, got %s", err.Error())
	}
}

func TestListModels_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected HTTP 500 in error, got %s", err.Error())
	}
}
