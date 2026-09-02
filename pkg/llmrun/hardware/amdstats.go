package hardware

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// drmRoot is the sysfs DRM class directory; a variable so tests can supply a
// fixture tree.
var drmRoot = "/sys/class/drm"

// GPUStats is point-in-time accelerator telemetry: the numbers a benchmark
// harness needs to say whether a run was thermally clean and whether the GPU was
// actually busy.
//
// MemoryUsedMB is deliberately absent for AMD. On a UMA APU neither sysfs
// counter is an honest analogue of NVIDIA's memory.used: mem_info_vram_total is
// the 512 MiB BIOS carve-out, and mem_info_gtt_used counts the whole shared
// pool's activity (~61.8 GiB on an idle box). Reporting either as "GPU memory
// used" would put a confidently wrong number in a benchmark result, which is
// worse than reporting none.
type GPUStats struct {
	// UtilizationPct is the busy percentage of the graphics pipeline.
	UtilizationPct float64
	// TemperatureC is the edge temperature in degrees Celsius.
	TemperatureC float64
	// PowerW is average package power draw in watts.
	PowerW float64
	// HasTemperature and HasPower record whether the sensor was actually
	// readable, so a genuine 0 is distinguishable from a missing reading.
	HasTemperature bool
	HasPower       bool
}

// AMDGPUStats reads telemetry for the first AMD GPU from the amdgpu driver's
// sysfs nodes. It reports ok=false when no AMD GPU is present.
//
// Like KFD detection this needs no ROCm userspace: rocm-smi lives inside the
// inference container on an appliance host, but the kernel driver publishes
// these counters to anyone.
func AMDGPUStats() (GPUStats, bool) {
	return amdGPUStatsFrom(drmRoot)
}

func amdGPUStatsFrom(root string) (GPUStats, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return GPUStats{}, false
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sortCardNames(names)

	for _, name := range names {
		if !isCardDir(name) {
			continue
		}
		dev := filepath.Join(root, name, "device")
		if !isAMDDevice(dev) {
			continue
		}
		var s GPUStats
		if v, ok := readSysfsFloat(filepath.Join(dev, "gpu_busy_percent")); ok {
			s.UtilizationPct = v
		}
		if hw, ok := findHwmon(dev); ok {
			// Kernel hwmon reports millidegrees and microwatts.
			if v, ok := readSysfsFloat(filepath.Join(hw, "temp1_input")); ok {
				s.TemperatureC = v / 1000.0
				s.HasTemperature = true
			}
			if v, ok := readSysfsFloat(filepath.Join(hw, "power1_average")); ok {
				s.PowerW = v / 1_000_000.0
				s.HasPower = true
			}
		}
		return s, true
	}
	return GPUStats{}, false
}

// isCardDir matches "cardN" while excluding connector directories such as
// "card1-DP-1", which sit in the same class directory.
func isCardDir(name string) bool {
	if !strings.HasPrefix(name, "card") {
		return false
	}
	suffix := name[len("card"):]
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sortCardNames orders cardN entries numerically so card10 does not precede
// card2.
func sortCardNames(names []string) {
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		na, aok := cardIndex(a)
		nb, bok := cardIndex(b)
		if aok && bok {
			return na < nb
		}
		return a < b
	})
}

func cardIndex(name string) (int, bool) {
	if !isCardDir(name) {
		return 0, false
	}
	n, err := strconv.Atoi(name[len("card"):])
	if err != nil {
		return 0, false
	}
	return n, true
}

func isAMDDevice(deviceDir string) bool {
	data, err := os.ReadFile(filepath.Join(deviceDir, "vendor"))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(data)), "0x1002")
}

// findHwmon returns the device's hwmon directory, whose numbering is assigned by
// the kernel at probe time and so cannot be hardcoded.
func findHwmon(deviceDir string) (string, bool) {
	base := filepath.Join(deviceDir, "hwmon")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hwmon") {
			return filepath.Join(base, e.Name()), true
		}
	}
	return "", false
}

func readSysfsFloat(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
