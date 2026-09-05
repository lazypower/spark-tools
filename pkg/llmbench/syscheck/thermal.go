package syscheck

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmrun/hardware"
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
		result.Message = "GPU temperature not available (no nvidia-smi, and no AMD GPU in sysfs)"
		result.Warning = "Cannot check thermal state without a readable GPU temperature sensor"
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

// gpuTemperature reads the GPU edge temperature, trying NVIDIA first and
// falling back to the amdgpu driver's hwmon sensor.
//
// Without the fallback an AMD box silently loses thermal gating: the check
// degrades to a warning and a benchmark suite whose whole premise is
// reproducibility will happily run on a throttling GPU.
func gpuTemperature() (float64, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		if s, ok := hardware.AMDGPUStats(); ok && s.HasTemperature {
			return s.TemperatureC, nil
		}
		return 0, fmt.Errorf("nvidia-smi: %w", err)
	}
	s := strings.TrimSpace(string(out))
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("no temperature data")
	}
	return strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
}
