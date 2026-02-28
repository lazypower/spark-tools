package job

import (
	"context"
	"fmt"
)

// sendWarmupPrompts sends N warmup prompts, discarding results.
func sendWarmupPrompts(ctx context.Context, endpoint string, prompts []string, count int, maxTokens int, temperature float64) error {
	for i := 0; i < count && i < len(prompts); i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result := probeRequest(ctx, endpoint, prompts[i], maxTokens, temperature)
		if result.Err != nil {
			return fmt.Errorf("warmup prompt %d: %w", i+1, result.Err)
		}
	}
	return nil
}
