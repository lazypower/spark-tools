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
		// The RAW version output is kept, not just the parsed version string.
		// llama.cpp announces its backend in the ggml init lines it writes to
		// stderr on any invocation ("ggml_cuda_init: found 1 ROCm devices"),
		// and --help carries none of it: a current llama-server --help is 730
		// lines of flag reference with no mention of ROCm, HIP, gfx or CUDA.
		// Detecting the backend from help text alone therefore always answers
		// "cpu", which then refuses GPU offload on a perfectly good GPU build.
		var backendText string
		if raw, err := probeVersionRaw(probeBin); err == nil {
			caps.Version = ParseVersionOutput(raw)
			backendText = raw
		}
		if helpText, err := probeHelp(probeBin); err == nil {
			parseCapabilities(helpText, caps)
			backendText += "\n" + helpText
		}

		// Ask the binary what devices it can actually use. This is the
		// authoritative answer and the only one that holds on current builds:
		// when a GPU initializes successfully llama.cpp prints NOTHING about
		// its backend in --version or --help, so sniffing that prose reports
		// "cpu" for a perfectly good GPU build and then refuses offload. The
		// backend name is the device prefix ("ROCm0:", "CUDA0:").
		if devices, err := probeDevices(probeBin); err == nil {
			if b := DetectBackendFromDevices(devices); b != "" {
				caps.Backend = b
				applyBackendArch(devices+"\n"+backendText, caps)
			} else if devicesListed(devices) {
				// The binary answered and listed no usable device. That is a
				// real CPU-only answer, not a failed probe.
				caps.Backend = "cpu"
			}
		} else if backendText != "" {
			// Older builds without --list-devices: fall back to the prose.
			caps.Backend = DetectBackend(backendText)
			applyBackendArch(backendText, caps)
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

// probeVersionRaw runs `binary --version` and returns its COMBINED output
// unparsed, because the backend marker lives in the ggml init lines on stderr
// rather than in the version string itself.
func probeVersionRaw(binaryPath string) (string, error) {
	out, err := exec.Command(binaryPath, "--version").CombinedOutput()
	if err != nil {
		// llama.cpp reports a non-zero exit for --version on some builds while
		// still printing everything we need.
		if len(out) > 0 {
			return string(out), nil
		}
		return "", fmt.Errorf("version probe failed: %w", err)
	}
	return string(out), nil
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

	// Backend and accelerator arch are resolved by the caller over the combined
	// version+help text (applyBackendArch), because --help alone carries no
	// backend marker on current builds.
	caps.Backend = DetectBackend(helpText)
	applyBackendArch(helpText, caps)
}

// rocmMarker matches the fingerprints of a ROCm/HIP llama.cpp build: the ROCm
// name itself, the HIP runtime, hipBLAS, or a gfx target string.
//
// The markers are word-bounded on purpose. A bare "HIP" substring match would
// fire on ordinary words in help text -- "chipset" contains it -- and
// misclassify a CPU build as a GPU one.
var rocmMarker = regexp.MustCompile(`\bROCM\b|\bHIP\b|\bHIPBLAS\b|\bGFX[0-9A-F]{3,4}\b`)

// DetectBackend determines the llama.cpp backend from version/help text.
// Exported for testing.
//
// ROCm is tested BEFORE CUDA, and the order is load-bearing. llama.cpp compiles
// its HIP backend from the same ggml-cuda sources, so a ROCm build's output
// still carries "CUDA" strings; checking CUDA first would classify every AMD
// build as CUDA. The converse never happens -- a genuine CUDA build does not
// mention ROCm, HIP, or a gfx target -- so leading with ROCm is safe.
func DetectBackend(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case rocmMarker.MatchString(upper):
		return "rocm"
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

// detectROCmArch tries to find an AMD gfx target string (e.g., "gfx1151") in the
// help/version text. It is the ROCm counterpart of detectCUDACompute.
func detectROCmArch(text string) string {
	re := regexp.MustCompile(`(?i)gfx[0-9a-f]{3,4}`)
	if m := re.FindString(text); m != "" {
		return strings.ToLower(m)
	}
	return ""
}

// applyBackendArch records the accelerator architecture the build targets, for
// whichever backend was detected.
func applyBackendArch(text string, caps *Capabilities) {
	switch caps.Backend {
	case "cuda":
		if cc := detectCUDACompute(text); cc != "" {
			caps.CUDACompute = cc
		}
	case "rocm":
		if arch := detectROCmArch(text); arch != "" {
			caps.ROCmArch = arch
		}
	}
}

// deviceLine matches an entry from `llama-server --list-devices`, whose backend
// prefix names the backend:
//
//	ROCm0: Radeon 8060S Graphics (64038 MiB, 64034 MiB free)
//	CUDA0: NVIDIA GB10 (131072 MiB, 130000 MiB free)
var deviceLine = regexp.MustCompile(`(?m)^\s+([A-Za-z]+)\d+:\s`)

// probeDevices runs `binary --list-devices` and returns its combined output.
func probeDevices(binaryPath string) (string, error) {
	out, err := exec.Command(binaryPath, "--list-devices").CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return string(out), nil
		}
		return "", fmt.Errorf("device probe failed: %w", err)
	}
	return string(out), nil
}

// devicesListed reports whether the output is a real device listing, so an
// empty list can be distinguished from a binary that does not support the flag.
func devicesListed(out string) bool {
	return strings.Contains(strings.ToLower(out), "available devices")
}

// DetectBackendFromDevices returns the backend named by the first device entry,
// or "" when the output lists no devices (or is not a device listing at all).
// Exported for testing.
func DetectBackendFromDevices(out string) string {
	if !devicesListed(out) {
		return ""
	}
	m := deviceLine.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}
