package download

import (
	"io"
	"sync"
	"time"
)

// tokenBucket implements a simple token-bucket rate limiter shared across
// multiple goroutines. Tokens represent bytes; the bucket refills at
// `rate` bytes per second.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64 // bytes per second
	capacity float64
	lastFill time.Time
}

func newTokenBucket(bytesPerSec int64) *tokenBucket {
	rate := float64(bytesPerSec)
	return &tokenBucket{
		tokens:   rate, // start with one second of burst
		rate:     rate,
		capacity: rate, // max burst = 1 second of bandwidth
		lastFill: time.Now(),
	}
}

// take blocks until n tokens (bytes) are available, then consumes them.
// Returns the number of tokens actually consumed (may be less than n
// if capacity is smaller, but always >= 1).
func (tb *tokenBucket) take(n int) int {
	for {
		tb.mu.Lock()
		tb.refill()

		want := float64(n)
		if want > tb.capacity {
			want = tb.capacity
		}

		if tb.tokens >= want {
			tb.tokens -= want
			tb.mu.Unlock()
			return int(want)
		}

		// How long until enough tokens are available?
		deficit := want - tb.tokens
		wait := time.Duration(deficit / tb.rate * float64(time.Second))
		tb.mu.Unlock()

		time.Sleep(wait)
	}
}

func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastFill).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastFill = now
}

// rateLimitedReader wraps an io.Reader and throttles reads through a
// shared token bucket.
type rateLimitedReader struct {
	r      io.Reader
	bucket *tokenBucket
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	// Throttle in increments to avoid reading huge buffers at once.
	allowed := r.bucket.take(len(p))
	n, err := r.r.Read(p[:allowed])
	return n, err
}
