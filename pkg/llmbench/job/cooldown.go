package job

import (
	"context"
	"time"
)

// Cooldown waits for the specified duration with context cancellation support.
func Cooldown(ctx context.Context, seconds int) error {
	if seconds <= 0 {
		return nil
	}

	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
