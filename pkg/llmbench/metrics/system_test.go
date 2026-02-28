package metrics

import (
	"context"
	"testing"
	"time"
)

func TestSystemSampler_SparseSamples(t *testing.T) {
	// If fewer than 3 samples, Available should be false
	s := NewSystemSampler(500)
	s.samples = []systemSample{
		{cpuPercent: 10},
	}
	s.done = make(chan struct{})
	close(s.done) // Already closed, Stop won't block

	result := s.Stop()
	if result.Available {
		t.Error("should not be available with < 3 samples")
	}
	if result.SampleCount != 1 {
		t.Errorf("sample count: got %d, want 1", result.SampleCount)
	}
}

func TestSystemSampler_Lifecycle(t *testing.T) {
	s := NewSystemSampler(50) // 50ms interval for fast test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Let it sample for a bit
	time.Sleep(200 * time.Millisecond)

	result := s.Stop()

	// Should have collected some samples (at least a few with 50ms interval over 200ms)
	if result.SampleCount == 0 {
		t.Error("expected some samples to be collected")
	}
	if result.SampleIntervalMs != 50 {
		t.Errorf("interval: got %d, want 50", result.SampleIntervalMs)
	}
}

func TestNewSystemSampler_DefaultInterval(t *testing.T) {
	s := NewSystemSampler(0)
	if s.intervalMs != 500 {
		t.Errorf("default interval: got %d, want 500", s.intervalMs)
	}
}
