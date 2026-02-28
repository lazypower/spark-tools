package syscheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ResourceCheck verifies sufficient resources for benchmarking.
type ResourceCheck struct {
	MinFreeMemoryMB int64 // Minimum free memory in MB (default: 4096)
	MinFreeDiskMB   int64 // Minimum free disk in MB for results (default: 1024)
	ResultDir       string
}

// DefaultResourceCheck returns a ResourceCheck with default thresholds.
func DefaultResourceCheck() ResourceCheck {
	return ResourceCheck{
		MinFreeMemoryMB: 4096,
		MinFreeDiskMB:   1024,
	}
}

// Run performs resource checks and returns a CheckResult.
func (c ResourceCheck) Run(_ context.Context) CheckResult {
	if c.MinFreeMemoryMB == 0 {
		c.MinFreeMemoryMB = 4096
	}
	if c.MinFreeDiskMB == 0 {
		c.MinFreeDiskMB = 1024
	}

	result := CheckResult{Name: "resources"}
	var messages []string

	// Memory check
	freeMB, err := freeMemoryMB()
	if err != nil {
		result.Warning = fmt.Sprintf("Could not check free memory: %v", err)
	} else if freeMB < c.MinFreeMemoryMB {
		result.Failed = true
		result.Message = fmt.Sprintf("Insufficient memory: %d MB free, need %d MB", freeMB, c.MinFreeMemoryMB)
		return result
	} else {
		messages = append(messages, fmt.Sprintf("Sufficient memory (%d MB available)", freeMB))
	}

	// Disk check
	if c.ResultDir != "" {
		freeDiskMB, err := freeDiskSpaceMB(c.ResultDir)
		if err != nil {
			result.Warning = fmt.Sprintf("Could not check disk space: %v", err)
		} else if freeDiskMB < c.MinFreeDiskMB {
			result.Failed = true
			result.Message = fmt.Sprintf("Insufficient disk space: %d MB free, need %d MB", freeDiskMB, c.MinFreeDiskMB)
			return result
		} else {
			messages = append(messages, fmt.Sprintf("Sufficient disk space (%d MB free)", freeDiskMB))
		}
	}

	// Check for llama.cpp binaries
	if _, err := exec.LookPath("llama-server"); err != nil {
		result.Failed = true
		result.Message = "llama-server not found in PATH"
		return result
	}
	messages = append(messages, "llama-server found")

	result.Message = strings.Join(messages, "; ")
	return result
}

func freeMemoryMB() (int64, error) {
	switch runtime.GOOS {
	case "darwin":
		return freeMemoryDarwin()
	case "linux":
		return freeMemoryLinux()
	default:
		return 0, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func freeMemoryDarwin() (int64, error) {
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	total, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, err
	}
	// On macOS, "free" memory is approximate — use vm_stat for better accuracy
	// but as a rough estimate, report total/2 if we can't get vm_stat
	cmd = exec.Command("vm_stat")
	out, err = cmd.Output()
	if err != nil {
		return total / (1024 * 1024 * 2), nil
	}
	var freePages, inactivePages int64
	pageSize := int64(os.Getpagesize())
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Pages free:") {
			freePages = parseVMStatValue(line)
		}
		if strings.Contains(line, "Pages inactive:") {
			inactivePages = parseVMStatValue(line)
		}
	}
	freeMB := (freePages + inactivePages) * pageSize / (1024 * 1024)
	return freeMB, nil
}

func parseVMStatValue(line string) int64 {
	parts := strings.Split(line, ":")
	if len(parts) < 2 {
		return 0
	}
	s := strings.TrimSpace(parts[1])
	s = strings.TrimSuffix(s, ".")
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func freeMemoryLinux() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kB, _ := strconv.ParseInt(fields[1], 10, 64)
				return kB / 1024, nil
			}
		}
	}
	return 0, fmt.Errorf("MemAvailable not found in /proc/meminfo")
}

func freeDiskSpaceMB(path string) (int64, error) {
	// Ensure the directory exists or use its parent
	dir := path
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := strings.TrimRight(dir, "/")
		idx := strings.LastIndex(parent, "/")
		if idx <= 0 {
			dir = "/"
			break
		}
		dir = parent[:idx]
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("statfs: %w", err)
	}
	freeMB := int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024)
	return freeMB, nil
}
