package metrics

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemSampler periodically samples CPU/memory/GPU metrics during
// the measurement phase of a benchmark job.
type SystemSampler struct {
	intervalMs int
	mu         sync.Mutex
	samples    []systemSample
	cancel     context.CancelFunc
	done       chan struct{}
}

type systemSample struct {
	cpuPercent   float64
	memoryMB     int64
	gpuPercent   float64
	gpuMemoryMB  int64
	gpuTemp      float64
}

// NewSystemSampler creates a sampler with the given interval in milliseconds.
func NewSystemSampler(intervalMs int) *SystemSampler {
	if intervalMs <= 0 {
		intervalMs = 500
	}
	return &SystemSampler{
		intervalMs: intervalMs,
		done:       make(chan struct{}),
	}
}

// Start begins periodic sampling in a background goroutine.
func (s *SystemSampler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.loop(ctx)
}

// Stop ends sampling and returns the collected SystemMetrics.
func (s *SystemSampler) Stop() *SystemMetrics {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.samples) < 3 {
		return &SystemMetrics{
			Available:        false,
			SampleCount:      len(s.samples),
			SampleIntervalMs: s.intervalMs,
		}
	}

	var (
		sumCPU      float64
		sumGPU      float64
		peakMem     int64
		peakGPUMem  int64
		peakGPU     float64
		throttled   bool
	)

	for _, sample := range s.samples {
		sumCPU += sample.cpuPercent
		sumGPU += sample.gpuPercent
		if sample.memoryMB > peakMem {
			peakMem = sample.memoryMB
		}
		if sample.gpuMemoryMB > peakGPUMem {
			peakGPUMem = sample.gpuMemoryMB
		}
		if sample.gpuPercent > peakGPU {
			peakGPU = sample.gpuPercent
		}
		if sample.gpuTemp > 90 {
			throttled = true
		}
	}

	n := float64(len(s.samples))
	return &SystemMetrics{
		Available:        true,
		PeakMemoryMB:     peakMem,
		PeakGPUMemoryMB:  peakGPUMem,
		MeanCPUPercent:   sumCPU / n,
		MeanGPUPercent:   sumGPU / n,
		PeakGPUPercent:   peakGPU,
		ThermalThrottled: throttled,
		SampleCount:      len(s.samples),
		SampleIntervalMs: s.intervalMs,
	}
}

func (s *SystemSampler) loop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(time.Duration(s.intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample := s.takeSample()
			s.mu.Lock()
			s.samples = append(s.samples, sample)
			s.mu.Unlock()
		}
	}
}

func (s *SystemSampler) takeSample() systemSample {
	var sample systemSample

	// Memory - best effort
	sample.memoryMB = sampleMemory()

	// GPU metrics from nvidia-smi - best effort
	gpu, gpuMem, gpuTemp := sampleNvidiaSMI()
	sample.gpuPercent = gpu
	sample.gpuMemoryMB = gpuMem
	sample.gpuTemp = gpuTemp

	return sample
}

func sampleMemory() int64 {
	switch runtime.GOOS {
	case "linux":
		return sampleMemoryLinux()
	default:
		return 0
	}
}

func sampleMemoryLinux() int64 {
	cmd := exec.Command("cat", "/proc/meminfo")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				total, _ := strconv.ParseInt(fields[1], 10, 64)
				return total / 1024 // KB to MB
			}
		}
	}
	return 0
}

func sampleNvidiaSMI() (gpuPct float64, gpuMemMB int64, gpuTemp float64) {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,temperature.gpu",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, 0, 0
	}
	fields := strings.Split(lines[0], ", ")
	if len(fields) >= 3 {
		gpuPct, _ = strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		mem, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		gpuMemMB = mem
		gpuTemp, _ = strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
	}
	return
}
