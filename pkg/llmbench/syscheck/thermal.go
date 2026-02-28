package syscheck

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ThermalCheck verifies the system is not thermally throttled.
type ThermalCheck struct {
	MaxGPUTempC float64 // Warn if GPU temp exceeds this (default: 80C)
}

// DefaultThermalCheck returns a ThermalCheck with default thresholds.
func DefaultThermalCheck() ThermalCheck {
	return ThermalCheck{
		MaxGPUTempC: 80.0,
	}
}

// Run performs the thermal check and returns a CheckResult.
func (c ThermalCheck) Run(_ context.Context) CheckResult {
	if c.MaxGPUTempC == 0 {
		c.MaxGPUTempC = 80.0
	}

	result := CheckResult{Name: "thermal"}

	temp, err := gpuTemperature()
	if err != nil {
		result.Message = "GPU temperature not available (nvidia-smi not found)"
		result.Warning = "Cannot check thermal state without nvidia-smi"
		return result
	}

	if temp > c.MaxGPUTempC {
		result.Failed = true
		result.Message = fmt.Sprintf("GPU temperature at %.0f°C (threshold: %.0f°C) — thermal throttling likely", temp, c.MaxGPUTempC)
		return result
	}

	result.Message = fmt.Sprintf("No thermal throttling detected (GPU: %.0f°C)", temp)
	return result
}

func gpuTemperature() (float64, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi: %w", err)
	}
	s := strings.TrimSpace(string(out))
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("no temperature data")
	}
	return strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
}
