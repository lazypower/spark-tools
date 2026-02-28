// Package hardware detects CPU, memory, GPU, and NUMA topology
// for automatic inference parameter tuning.
package hardware

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// HardwareInfo describes the detected hardware.
type HardwareInfo struct {
	CPUName       string    `json:"cpuName"`
	CPUCores      int       `json:"cpuCores"`
	TotalMemoryGB float64   `json:"totalMemoryGB"`
	FreeMemoryGB  float64   `json:"freeMemoryGB"`
	GPUs          []GPUInfo `json:"gpus,omitempty"`
	IsDGXSpark    bool      `json:"isDGXSpark"`
	NUMANodes     int       `json:"numaNodes"`
}

// GPUInfo describes a detected GPU.
type GPUInfo struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	MemoryGB float64 `json:"memoryGB"`
	Compute  string  `json:"compute,omitempty"` // e.g. "sm_100" for GB10
}

// DetectHardware probes the current system for CPU, memory, GPU, and NUMA
// topology. Detection is best-effort: failures in any subsystem produce
// zero values rather than errors, unless all detection fails.
func DetectHardware() (*HardwareInfo, error) {
	hw := &HardwareInfo{}

	// CPU detection
	hw.CPUCores = detectCPUCores()
	hw.CPUName = detectCPUName()

	// Memory detection
	hw.TotalMemoryGB, hw.FreeMemoryGB = detectMemory()

	// GPU detection (best-effort via nvidia-smi)
	hw.GPUs = detectGPUs()

	// NUMA detection (Linux only)
	hw.NUMANodes = detectNUMANodes()

	// DGX Spark detection
	hw.IsDGXSpark = IsDGXSpark(hw)

	return hw, nil
}

// detectCPUCores returns the number of logical CPU cores via runtime.NumCPU.
// This returns logical cores (including hyperthreads); for physical cores
// we would need platform-specific parsing, but runtime.NumCPU is a safe
// cross-platform baseline.
func detectCPUCores() int {
	return runtime.NumCPU()
}

// detectCPUName reads the CPU model name from the system.
// On Linux it parses /proc/cpuinfo; on macOS it uses sysctl.
func detectCPUName() string {
	switch runtime.GOOS {
	case "linux":
		return detectCPUNameLinux()
	case "darwin":
		return detectCPUNameDarwin()
	default:
		return runtime.GOARCH
	}
}

// detectCPUNameLinux parses /proc/cpuinfo for the "model name" field.
func detectCPUNameLinux() string {
	out, err := exec.Command("cat", "/proc/cpuinfo").Output()
	if err != nil {
		return "unknown"
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
		// ARM (aarch64) uses "Model" or "CPU implementer" lines;
		// check for a "Model" field as fallback.
		if strings.HasPrefix(line, "Model") && !strings.HasPrefix(line, "Model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

// detectCPUNameDarwin uses sysctl to read the CPU brand string on macOS.
func detectCPUNameDarwin() string {
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "unknown"
	}
	return name
}

// detectMemory returns (totalGB, freeGB) for the current system.
// On Linux it parses /proc/meminfo; on macOS it uses sysctl and vm_stat.
func detectMemory() (totalGB, freeGB float64) {
	switch runtime.GOOS {
	case "linux":
		return detectMemoryLinux()
	case "darwin":
		return detectMemoryDarwin()
	default:
		return 0, 0
	}
}

// detectMemoryLinux parses /proc/meminfo for MemTotal and MemAvailable.
func detectMemoryLinux() (totalGB, freeGB float64) {
	out, err := exec.Command("cat", "/proc/meminfo").Output()
	if err != nil {
		return 0, 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	var totalKB, availKB int64
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalKB = parseMemInfoValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availKB = parseMemInfoValue(line)
		}
	}
	totalGB = float64(totalKB) / (1024 * 1024)
	freeGB = float64(availKB) / (1024 * 1024)
	return totalGB, freeGB
}

// parseMemInfoValue extracts the numeric kB value from a /proc/meminfo line.
// Example input: "MemTotal:       32661924 kB"
func parseMemInfoValue(line string) int64 {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0
	}
	val, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// detectMemoryDarwin uses sysctl for total memory and vm_stat for free memory.
func detectMemoryDarwin() (totalGB, freeGB float64) {
	// Total memory via sysctl
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0
	}
	totalBytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, 0
	}
	totalGB = float64(totalBytes) / (1024 * 1024 * 1024)

	// Free memory approximation via vm_stat
	// We consider "free + inactive" pages as available.
	vmOut, err := exec.Command("vm_stat").Output()
	if err != nil {
		return totalGB, 0
	}
	pageSize := int64(16384) // default on Apple Silicon
	// Try to read actual page size from vm_stat header
	scanner := bufio.NewScanner(bytes.NewReader(vmOut))
	var freePages, inactivePages int64
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "page size of") {
			// "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "of" && i+1 < len(parts) {
					ps, err := strconv.ParseInt(parts[i+1], 10, 64)
					if err == nil {
						pageSize = ps
					}
				}
			}
		} else if strings.HasPrefix(line, "Pages free:") {
			freePages = parseVMStatValue(line)
		} else if strings.HasPrefix(line, "Pages inactive:") {
			inactivePages = parseVMStatValue(line)
		}
	}
	freeGB = float64((freePages+inactivePages)*pageSize) / (1024 * 1024 * 1024)
	return totalGB, freeGB
}

// parseVMStatValue extracts the numeric value from a vm_stat line.
// Example: "Pages free:                             1234."
func parseVMStatValue(line string) int64 {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return 0
	}
	s := strings.TrimSpace(parts[1])
	s = strings.TrimRight(s, ".")
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// detectGPUs queries nvidia-smi for GPU information.
// Returns nil (not an error) if nvidia-smi is unavailable.
func detectGPUs() []GPUInfo {
	// Query: index, name, memory.total (MiB), compute capability
	out, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=index,name,memory.total,compute_cap",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil
	}

	var gpus []GPUInfo
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		gpu, err := parseNvidiaSMILine(line)
		if err != nil {
			continue
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

// parseNvidiaSMILine parses a single CSV line from nvidia-smi output.
// Expected format: "0, NVIDIA GB10, 12288, 10.0"
func parseNvidiaSMILine(line string) (GPUInfo, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 3 {
		return GPUInfo{}, fmt.Errorf("expected at least 3 fields, got %d", len(fields))
	}

	idx, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return GPUInfo{}, fmt.Errorf("invalid GPU index: %w", err)
	}

	name := strings.TrimSpace(fields[1])

	memMiB, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
	if err != nil {
		return GPUInfo{}, fmt.Errorf("invalid GPU memory: %w", err)
	}

	gpu := GPUInfo{
		Index:    idx,
		Name:     name,
		MemoryGB: memMiB / 1024.0,
	}

	// Compute capability is optional (4th field).
	if len(fields) >= 4 {
		cc := strings.TrimSpace(fields[3])
		if cc != "" {
			// Convert "10.0" to "sm_100"
			gpu.Compute = formatComputeCapability(cc)
		}
	}

	return gpu, nil
}

// formatComputeCapability converts nvidia-smi's "major.minor" format
// to the "sm_XY" notation (e.g., "10.0" -> "sm_100").
func formatComputeCapability(cc string) string {
	parts := strings.SplitN(cc, ".", 2)
	if len(parts) != 2 {
		return "sm_" + cc
	}
	major := strings.TrimSpace(parts[0])
	minor := strings.TrimSpace(parts[1])
	return fmt.Sprintf("sm_%s%s", major, minor)
}

// detectNUMANodes counts the number of NUMA nodes on the system.
// Only supported on Linux via /sys/devices/system/node/.
func detectNUMANodes() int {
	if runtime.GOOS != "linux" {
		return 0
	}

	out, err := exec.Command("ls", "-d", "/sys/devices/system/node/node*").Output()
	if err != nil {
		// Try alternative: lscpu
		return detectNUMANodesVialspu()
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// detectNUMANodesVialspu attempts to detect NUMA nodes via lscpu output.
func detectNUMANodesVialspu() int {
	out, err := exec.Command("lscpu").Output()
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "NUMA node(s):") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				val, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err == nil {
					return val
				}
			}
		}
	}
	return 0
}
