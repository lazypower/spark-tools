package metrics

import (
	"context"
	"os"
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

	// CPU utilization is a rate, computed from the delta of /proc/stat jiffies
	// between ticks. These carry the previous reading; only the loop goroutine
	// touches them, so they need no lock.
	prevCPUTotal int64
	prevCPUIdle  int64

	// cpuOK/memOK record whether CPU and memory were ever validly sampled on
	// this platform, so aggregation can report Available honestly instead of
	// presenting fabricated zeros.
	cpuOK bool
	memOK bool
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

	// Report the block as available only when we have enough samples AND CPU and
	// memory were actually measured on this platform — never present a fabricated
	// mean CPU of 0 or a memory figure that isn't real usage.
	if len(s.samples) < 3 || !s.cpuOK || !s.memOK {
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

	// Establish the CPU baseline before the first tick so the first recorded
	// sample already reflects a real delta rather than a spurious 0.
	s.prevCPUTotal, s.prevCPUIdle, _ = readCPUTimes()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample, cpuOK, memOK := s.takeSample()
			s.mu.Lock()
			s.samples = append(s.samples, sample)
			if cpuOK {
				s.cpuOK = true
			}
			if memOK {
				s.memOK = true
			}
			s.mu.Unlock()
		}
	}
}

func (s *SystemSampler) takeSample() (sample systemSample, cpuOK, memOK bool) {
	// CPU utilization since the previous tick.
	sample.cpuPercent, cpuOK = s.sampleCPU()

	// Memory actually in use (not total RAM).
	sample.memoryMB, memOK = sampleUsedMemoryMB()

	// GPU metrics from nvidia-smi - best effort, not gated on availability.
	gpu, gpuMem, gpuTemp := sampleNvidiaSMI()
	sample.gpuPercent = gpu
	sample.gpuMemoryMB = gpuMem
	sample.gpuTemp = gpuTemp

	return sample, cpuOK, memOK
}

// sampleCPU returns busy CPU percent since the previous reading, updating the
// stored baseline. The second return is false when CPU times can't be read
// (non-Linux, or /proc unavailable).
func (s *SystemSampler) sampleCPU() (float64, bool) {
	total, idle, ok := readCPUTimes()
	if !ok {
		return 0, false
	}
	dTotal := total - s.prevCPUTotal
	dIdle := idle - s.prevCPUIdle
	s.prevCPUTotal = total
	s.prevCPUIdle = idle
	if dTotal <= 0 {
		return 0, true // no elapsed jiffies yet; valid but idle
	}
	busy := float64(dTotal-dIdle) / float64(dTotal) * 100
	if busy < 0 {
		busy = 0
	}
	if busy > 100 {
		busy = 100
	}
	return busy, true
}

// readCPUTimes reads aggregate CPU jiffies from the first line of /proc/stat:
//
//	cpu user nice system idle iowait irq softirq steal ...
//
// Returns (total, idle, ok). idle includes iowait. ok is false off Linux.
func readCPUTimes() (total, idle int64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, f := range fields[1:] {
		v, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 || i == 4 { // idle, iowait
			idle += v
		}
	}
	return total, idle, true
}

// sampleUsedMemoryMB returns memory in use (MemTotal - MemAvailable) in MB. The
// second return is false when it can't be read (non-Linux, or /proc missing).
func sampleUsedMemoryMB() (int64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	var total, avail int64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = strconv.ParseInt(fields[1], 10, 64)
			haveTotal = true
		case "MemAvailable:":
			avail, _ = strconv.ParseInt(fields[1], 10, 64)
			haveAvail = true
		}
	}
	if !haveTotal || !haveAvail {
		return 0, false
	}
	usedKB := total - avail
	if usedKB < 0 {
		usedKB = 0
	}
	return usedKB / 1024, true // KB to MB
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
