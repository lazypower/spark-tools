// Package engine manages llama.cpp binary detection, process launch,
// and lifecycle management.
package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// knownBinaries lists the llama.cpp binaries we search for.
var knownBinaries = []string{"llama-server", "llama-cli", "llama-bench"}

// commonPaths are fallback directories to search for llama.cpp binaries.
var commonPaths = []string{"/usr/local/bin", "/opt/llama.cpp/bin"}

// DetectBinaries finds llama.cpp binaries on the system and probes their capabilities.
//
// Search order:
//  1. llamaDir (if provided) — explicit directory override
//  2. $PATH — standard mechanism
//  3. Common install locations: /usr/local/bin, /opt/llama.cpp/bin
//
// Returns a Capabilities struct describing the detected build. If no binaries
// are found, returns an error.
func DetectBinaries(llamaDir string) (*Capabilities, error) {
	binDir, found := findBinaryDir(llamaDir)
	if !found {
		return nil, fmt.Errorf("llama.cpp binaries not found. Set LLM_RUN_LLAMA_DIR or ensure llama-server/llama-cli are on $PATH")
	}

	caps := &Capabilities{
		Backend:   "cpu",
		BinaryDir: binDir,
	}

	// Check which binaries are present.
	serverPath := lookupBinary(binDir, "llama-server")
	cliPath := lookupBinary(binDir, "llama-cli")
	benchPath := lookupBinary(binDir, "llama-bench")

	caps.ServerMode = serverPath != ""
	caps.BenchMode = benchPath != ""

	// Determine primary binary path (prefer llama-server, fall back to llama-cli).
	if serverPath != "" {
		caps.BinaryPath = serverPath
	} else if cliPath != "" {
		caps.BinaryPath = cliPath
	} else if benchPath != "" {
		caps.BinaryPath = benchPath
	} else {
		return nil, fmt.Errorf("no usable llama.cpp binaries found in %s", binDir)
	}

	// Probe version from llama-server (or llama-cli as fallback).
	probeBin := serverPath
	if probeBin == "" {
		probeBin = cliPath
	}
	if probeBin != "" {
		if ver, err := probeVersion(probeBin); err == nil {
			caps.Version = ver
		}
		if helpText, err := probeHelp(probeBin); err == nil {
			parseCapabilities(helpText, caps)
		}
	}

	return caps, nil
}

// findBinaryDir locates the directory containing llama.cpp binaries.
// It searches in priority order: llamaDir, $PATH, common paths.
// Returns the directory path and whether any binary was found.
func findBinaryDir(llamaDir string) (string, bool) {
	// 1. Explicit directory override.
	if llamaDir != "" {
		for _, name := range knownBinaries {
			candidate := filepath.Join(llamaDir, name)
			if isExecutable(candidate) {
				return llamaDir, true
			}
		}
	}

	// 2. Search $PATH.
	for _, name := range knownBinaries {
		if p, err := exec.LookPath(name); err == nil {
			return filepath.Dir(p), true
		}
	}

	// 3. Common install locations.
	for _, dir := range commonPaths {
		for _, name := range knownBinaries {
			candidate := filepath.Join(dir, name)
			if isExecutable(candidate) {
				return dir, true
			}
		}
	}

	return "", false
}

// lookupBinary checks if a specific binary exists in the given directory
// and returns its full path, or empty string if not found.
func lookupBinary(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if isExecutable(candidate) {
		return candidate
	}
	return ""
}

// isExecutable checks whether the given path is an executable file.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0111 != 0
}

// probeVersion runs `binary --version` and parses the version string.
func probeVersion(binaryPath string) (string, error) {
	out, err := exec.Command(binaryPath, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("version probe failed: %w", err)
	}
	return ParseVersionOutput(string(out)), nil
}

// probeHelp runs `binary --help` and returns the help text.
func probeHelp(binaryPath string) (string, error) {
	out, err := exec.Command(binaryPath, "--help").CombinedOutput()
	if err != nil {
		// Some builds return non-zero exit from --help; still use the output.
		if len(out) > 0 {
			return string(out), nil
		}
		return "", fmt.Errorf("help probe failed: %w", err)
	}
	return string(out), nil
}

// ParseVersionOutput extracts the version string from llama-server --version output.
// Exported for testing.
//
// Typical output formats:
//
//	"version: 1234 (abc1234)"
//	"llama-server version b1234 (commit abc1234)"
//	"v0.0.0-b1234+abc1234"
func ParseVersionOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "unknown"
	}

	// Try to match "version: <version>" or "version <version>" patterns.
	re := regexp.MustCompile(`(?i)version[:\s]+(\S+)`)
	if m := re.FindStringSubmatch(output); len(m) > 1 {
		return m[1]
	}

	// Try to match "b<number>" build number pattern.
	re2 := regexp.MustCompile(`\bb(\d{3,})\b`)
	if m := re2.FindString(output); m != "" {
		return m
	}

	// Fall back to first non-empty line.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "unknown"
}

// parseCapabilities examines --help output to determine which features
// the build supports, and detects the backend.
func parseCapabilities(helpText string, caps *Capabilities) {
	lower := strings.ToLower(helpText)

	caps.FlashAttention = strings.Contains(lower, "--flash-attn")
	caps.NUMA = strings.Contains(lower, "--numa")
	caps.MLock = strings.Contains(lower, "--mlock")
	caps.MMap = strings.Contains(lower, "--mmap")

	// Detect backend from help text or version string.
	caps.Backend = DetectBackend(helpText)

	// Try to detect CUDA compute capability.
	if caps.Backend == "cuda" {
		if cc := detectCUDACompute(helpText); cc != "" {
			caps.CUDACompute = cc
		}
	}
}

// DetectBackend determines the llama.cpp backend from version/help text.
// Exported for testing.
func DetectBackend(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, "CUDA"):
		return "cuda"
	case strings.Contains(upper, "METAL"):
		return "metal"
	case strings.Contains(upper, "VULKAN"):
		return "vulkan"
	default:
		return "cpu"
	}
}

// detectCUDACompute tries to find a CUDA compute capability string (e.g., "sm_100")
// in the help/version text.
func detectCUDACompute(text string) string {
	re := regexp.MustCompile(`sm_\d+`)
	if m := re.FindString(text); m != "" {
		return m
	}
	return ""
}
