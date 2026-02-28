package job

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmbench/metrics"
)

// probeResult holds metrics from a single prompt request.
type probeResult struct {
	Sample  metrics.RawSample
	Timings *metrics.Timings
	Err     error
}

// probeRequest sends a streaming chat completion request to llama-server,
// measures TTFT and end-to-end latency, and extracts timings from the
// final SSE chunk.
func probeRequest(ctx context.Context, endpoint string, prompt string, maxTokens int, temperature float64) probeResult {
	temp := &temperature
	reqBody := struct {
		Model       string      `json:"model"`
		Messages    []message   `json:"messages"`
		MaxTokens   int         `json:"max_tokens"`
		Temperature *float64    `json:"temperature"`
		Stream      bool        `json:"stream"`
	}{
		Model: "default",
		Messages: []message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return probeResult{Err: fmt.Errorf("marshaling request: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return probeResult{Err: fmt.Errorf("creating request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return probeResult{Err: fmt.Errorf("sending request: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return probeResult{Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	var (
		ttft         time.Duration
		firstToken   bool
		lastData     []byte
	)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		if !firstToken {
			ttft = time.Since(startTime)
			firstToken = true
		}
		lastData = []byte(data)
	}

	endToEnd := time.Since(startTime)

	if err := scanner.Err(); err != nil {
		return probeResult{Err: fmt.Errorf("reading stream: %w", err)}
	}

	sample := metrics.RawSample{
		TTFTMs:      float64(ttft.Microseconds()) / 1000.0,
		EndToEndMs:  float64(endToEnd.Microseconds()) / 1000.0,
		PromptBytes: len(prompt),
	}

	// Extract timings from the last SSE data chunk
	var timings *metrics.Timings
	if lastData != nil {
		timings = extractTimingsFromSSE(lastData)
		if timings != nil {
			sample.PromptTokens = timings.PromptN
			sample.PredictedTokens = timings.PredictedN
			sample.PromptMs = timings.PromptMs
			sample.PredictedMs = timings.PredictedMs
		}
	}

	return probeResult{
		Sample:  sample,
		Timings: timings,
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// extractTimingsFromSSE attempts to extract timings from a chat completion
// SSE data chunk. llama-server includes timings in the final chunk.
func extractTimingsFromSSE(data []byte) *metrics.Timings {
	var envelope struct {
		Timings *metrics.Timings `json:"timings"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	if envelope.Timings != nil {
		return envelope.Timings
	}
	// Fallback: construct from usage if timings not available
	if envelope.Usage != nil && envelope.Usage.CompletionTokens > 0 {
		return &metrics.Timings{
			PromptN:    envelope.Usage.PromptTokens,
			PredictedN: envelope.Usage.CompletionTokens,
		}
	}
	return nil
}
