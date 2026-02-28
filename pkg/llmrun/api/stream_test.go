package api

import (
	"strings"
	"testing"
)

// TestReadSSEStream_UsageFromFinalChunk is a regression test for the bug where
// readSSEStream always returned an empty Usage struct, discarding token counts
// sent by the server in the final SSE chunk.
func TestReadSSEStream_UsageFromFinalChunk(t *testing.T) {
	// Simulate a minimal SSE stream: one content delta, one final delta
	// with finish_reason and usage, then [DONE].
	stream := strings.Join([]string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
		``,
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c := &Client{}
	var tokens []string
	var lastFinishReason string

	usage, err := c.readSSEStream(strings.NewReader(stream), func(delta StreamDelta) {
		if len(delta.Choices) > 0 {
			if delta.Choices[0].Delta != nil && delta.Choices[0].Delta.Content != "" {
				tokens = append(tokens, delta.Choices[0].Delta.Content)
			}
			if delta.Choices[0].FinishReason != "" {
				lastFinishReason = delta.Choices[0].FinishReason
			}
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "hi" {
		t.Errorf("expected [hi], got %v", tokens)
	}
	if lastFinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %q", lastFinishReason)
	}
	if usage.PromptTokens != 5 {
		t.Errorf("expected 5 prompt tokens, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 1 {
		t.Errorf("expected 1 completion token, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 6 {
		t.Errorf("expected 6 total tokens, got %d", usage.TotalTokens)
	}
}

// TestReadSSEStream_TruncatedFinishReason verifies that finish_reason "length"
// (token limit hit) is correctly propagated to the handler.
func TestReadSSEStream_TruncatedFinishReason(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		``,
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":100,"total_tokens":110}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c := &Client{}
	var finishReason string

	usage, err := c.readSSEStream(strings.NewReader(stream), func(delta StreamDelta) {
		if len(delta.Choices) > 0 && delta.Choices[0].FinishReason != "" {
			finishReason = delta.Choices[0].FinishReason
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finishReason != "length" {
		t.Errorf("expected finish_reason=length, got %q", finishReason)
	}
	if usage.CompletionTokens != 100 {
		t.Errorf("expected 100 completion tokens, got %d", usage.CompletionTokens)
	}
}
