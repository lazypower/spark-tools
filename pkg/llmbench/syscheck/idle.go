// Package syscheck provides pre-flight system checks for benchmarks:
// idle state, thermal throttling, and resource availability.
package syscheck

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// IdleCheck verifies the system is reasonably idle for benchmarking.
type IdleCheck struct {
	MaxCPUPercent  float64       // Abort if system CPU > threshold (default: 15%)
	MaxGPUPercent  float64       // Abort if GPU utilization > threshold (default: 10%)
	SampleDuration time.Duration // How long to sample (default: 5s)
}

// DefaultIdleCheck returns an IdleCheck with default thresholds.
func DefaultIdleCheck() IdleCheck {
	return IdleCheck{
		MaxCPUPercent:  15.0,
		MaxGPUPercent:  10.0,
		SampleDuration: 5 * time.Second,
	}
}

// Run performs the idle check and returns a CheckResult.
func (c IdleCheck) Run(ctx context.Context) CheckResult {
	if c.MaxCPUPercent == 0 {
		c.MaxCPUPercent = 15.0
	}
	if c.MaxGPUPercent == 0 {
		c.MaxGPUPercent = 10.0
	}
	if c.SampleDuration == 0 {
		c.SampleDuration = 5 * time.Second
	}

	result := CheckResult{Name: "idle"}

	// CPU check
	cpuPct, err := sampleCPU(ctx, c.SampleDuration)
	if err != nil {
		result.Warning = fmt.Sprintf("Could not measure CPU utilization: %v", err)
		return result
	}

	if cpuPct > c.MaxCPUPercent {
		result.Failed = true
		result.Message = fmt.Sprintf("CPU utilization at %.1f%% (threshold: %.0f%%)", cpuPct, c.MaxCPUPercent)
		return result
	}
	result.Message = fmt.Sprintf("CPU idle (%.1f%% utilization)", cpuPct)

	// GPU check (optional)
	gpuPct, err := sampleGPU()
	if err == nil {
		if gpuPct > c.MaxGPUPercent {
			result.Failed = true
			result.Message = fmt.Sprintf("GPU utilization at %.1f%% (threshold: %.0f%%)", gpuPct, c.MaxGPUPercent)
			return result
		}
		result.Message += fmt.Sprintf(", GPU idle (%.1f%% utilization)", gpuPct)
	}

	return result
}

// sampleCPU measures average CPU utilization over the sample duration.
func sampleCPU(ctx context.Context, duration time.Duration) (float64, error) {
	switch runtime.GOOS {
	case "darwin":
		return sampleCPUDarwin(ctx, duration)
	case "linux":
		return sampleCPULinux(ctx, duration)
	default:
		return 0, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func sampleCPUDarwin(ctx context.Context, duration time.Duration) (float64, error) {
	// Use top in logging mode for a short sample
	samples := int(duration.Seconds())
	if samples < 1 {
		samples = 1
	}
	cmd := exec.CommandContext(ctx, "top", "-l", strconv.Itoa(samples+1), "-n", "0", "-s", "1")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("top: %w", err)
	}
	// Parse "CPU usage: X% user, Y% sys, Z% idle"
	var totalUsed float64
	var count int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "CPU usage:") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if strings.HasSuffix(p, "idle") && i > 0 {
					idleStr := strings.TrimSuffix(parts[i-1], "%")
					idle, err := strconv.ParseFloat(idleStr, 64)
					if err == nil {
						totalUsed += (100.0 - idle)
						count++
					}
				}
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("could not parse CPU usage from top output")
	}
	return totalUsed / float64(count), nil
}

func sampleCPULinux(ctx context.Context, duration time.Duration) (float64, error) {
	// Read /proc/stat twice and compute delta
	read := func() (idle, total uint64, err error) {
		cmd := exec.CommandContext(ctx, "cat", "/proc/stat")
		out, err := cmd.Output()
		if err != nil {
			return 0, 0, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					return 0, 0, fmt.Errorf("unexpected /proc/stat format")
				}
				var vals []uint64
				for _, f := range fields[1:] {
					v, _ := strconv.ParseUint(f, 10, 64)
					vals = append(vals, v)
				}
				for _, v := range vals {
					total += v
				}
				if len(vals) >= 4 {
					idle = vals[3]
				}
				return idle, total, nil
			}
		}
		return 0, 0, fmt.Errorf("/proc/stat: no cpu line found")
	}

	idle1, total1, err := read()
	if err != nil {
		return 0, err
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(duration):
	}

	idle2, total2, err := read()
	if err != nil {
		return 0, err
	}

	dTotal := float64(total2 - total1)
	dIdle := float64(idle2 - idle1)
	if dTotal == 0 {
		return 0, nil
	}
	return (1.0 - dIdle/dTotal) * 100.0, nil
}

// sampleGPU queries nvidia-smi for current GPU utilization.
func sampleGPU() (float64, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi not available: %w", err)
	}
	s := strings.TrimSpace(string(out))
	// Take first GPU if multiple lines
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("no GPU data")
	}
	return strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
}
