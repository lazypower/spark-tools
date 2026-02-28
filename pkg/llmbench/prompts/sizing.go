package prompts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// TokenizeResult holds tokenization results for a prompt.
type TokenizeResult struct {
	TokenCount int
	Source     string // "tokenized" or "estimated"
}

// Tokenize sends a prompt to llama-server's /tokenize endpoint for exact token counting.
func Tokenize(ctx context.Context, endpoint, prompt string) (*TokenizeResult, error) {
	body, err := json.Marshal(map[string]string{"content": prompt})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/tokenize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tokenize request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tokenize: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Tokens []int `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tokenize: decoding response: %w", err)
	}

	return &TokenizeResult{
		TokenCount: len(result.Tokens),
		Source:     "tokenized",
	}, nil
}

// EstimateTokens estimates the token count from byte length.
// Uses the common heuristic of ~4 bytes per token for English text.
func EstimateTokens(prompt string) *TokenizeResult {
	byteLen := len(prompt)
	// Rough heuristic: ~4 bytes per token for English text
	estimated := (byteLen + 3) / 4
	if estimated < 1 {
		estimated = 1
	}
	return &TokenizeResult{
		TokenCount: estimated,
		Source:     "estimated",
	}
}
