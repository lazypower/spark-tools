package metrics

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// Regression: the mean CPU and peak memory must reflect the samples. CPU was
// previously never sampled (mean always 0) and memory reported total RAM.
func TestSystemSampler_AggregatesRealSamples(t *testing.T) {
	s := NewSystemSampler(500)
	s.samples = []systemSample{
		{cpuPercent: 20, memoryMB: 1000},
		{cpuPercent: 40, memoryMB: 3000},
		{cpuPercent: 60, memoryMB: 2000},
	}
	s.cpuOK, s.memOK = true, true
	s.done = make(chan struct{})
	close(s.done)

	m := s.Stop()
	if !m.Available {
		t.Fatal("three real samples must be available")
	}
	if m.MeanCPUPercent != 40 {
		t.Errorf("mean CPU must aggregate the samples, got %v want 40", m.MeanCPUPercent)
	}
	if m.PeakMemoryMB != 3000 {
		t.Errorf("peak memory must be the max sample, got %d want 3000", m.PeakMemoryMB)
	}
}

// Honesty: when CPU/mem were never validly sampled (e.g. off Linux), the block
// must be Available:false with no fabricated aggregates — not a mean CPU of 0
// and total RAM presented as real.
func TestSystemSampler_UnavailableWhenNotSampled(t *testing.T) {
	s := NewSystemSampler(500)
	s.samples = []systemSample{{}, {}, {}}
	s.cpuOK, s.memOK = false, false
	s.done = make(chan struct{})
	close(s.done)

	m := s.Stop()
	if m.Available {
		t.Error("must not report available when CPU/mem were never sampled")
	}
	if m.MeanCPUPercent != 0 || m.PeakMemoryMB != 0 {
		t.Errorf("unavailable metrics must be zeroed, got cpu=%v mem=%d", m.MeanCPUPercent, m.PeakMemoryMB)
	}
}

func TestReadCPUTimes_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/stat is Linux-only")
	}
	total, idle, ok := readCPUTimes()
	if !ok || total <= 0 || idle <= 0 || idle > total {
		t.Errorf("bad CPU times: total=%d idle=%d ok=%v", total, idle, ok)
	}
}

func TestSampleUsedMemory_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("used-memory sampling is Linux-only")
	}
	used, ok := sampleUsedMemoryMB()
	if !ok || used <= 0 {
		t.Errorf("used memory must be a positive real value, got %d ok=%v", used, ok)
	}
}

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
