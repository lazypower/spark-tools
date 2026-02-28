package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WaitForReady polls the /health endpoint at the given endpoint URL until it
// returns status "ok", the context is cancelled, or the timeout expires.
//
// It polls every 250ms. The timeout is applied independently of the context;
// whichever deadline arrives first takes effect.
func WaitForReady(ctx context.Context, endpoint string, timeout time.Duration) error {
	if endpoint == "" {
		return fmt.Errorf("empty endpoint")
	}

	healthURL := endpoint + "/health"
	deadline := time.After(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	client := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for server: %w", ctx.Err())
		case <-deadline:
			return fmt.Errorf("timeout after %s waiting for server at %s to become ready", timeout, endpoint)
		case <-ticker.C:
			status, err := pollHealth(client, healthURL)
			if err != nil {
				// Server not up yet, keep polling.
				continue
			}
			if status.Status == "ok" {
				return nil
			}
			// Status is "loading" or something else — keep polling.
		}
	}
}

// pollHealth performs a single GET /health request and returns the parsed status.
func pollHealth(client *http.Client, healthURL string) (*HealthStatus, error) {
	resp, err := client.Get(healthURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("parsing health response: %w", err)
	}

	return &status, nil
}
