package engine

import (
	"testing"
)

func TestParseVersionOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard version format",
			input:    "version: 1234 (abc1234)",
			expected: "1234",
		},
		{
			name:     "version with prefix",
			input:    "llama-server version b3456 (commit abc1234)",
			expected: "b3456",
		},
		{
			name:     "version colon format",
			input:    "version: v0.1.0",
			expected: "v0.1.0",
		},
		{
			name:     "multiline output with version on first line",
			input:    "version: b4567\nbuilt with CUDA\n",
			expected: "b4567",
		},
		{
			name:     "build number only",
			input:    "some output b1234 more text",
			expected: "b1234",
		},
		{
			name:     "empty output",
			input:    "",
			expected: "unknown",
		},
		{
			name:     "whitespace only",
			input:    "   \n  \n  ",
			expected: "unknown",
		},
		{
			name:     "no recognizable version pattern",
			input:    "llama-server built on 2024-01-01",
			expected: "llama-server built on 2024-01-01",
		},
		{
			name:     "version uppercase",
			input:    "VERSION: 5.0.1",
			expected: "5.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseVersionOutput(tt.input)
			if got != tt.expected {
				t.Errorf("ParseVersionOutput(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDetectBackend(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "CUDA backend",
			text:     "llama-server version b1234 built with CUDA 12.2",
			expected: "cuda",
		},
		{
			name:     "Metal backend",
			text:     "Compiled with Metal support enabled",
			expected: "metal",
		},
		{
			name:     "Vulkan backend",
			text:     "Backend: Vulkan 1.3",
			expected: "vulkan",
		},
		{
			name:     "CPU only",
			text:     "llama-server version b1234 compiled for x86_64",
			expected: "cpu",
		},
		{
			name:     "empty text",
			text:     "",
			expected: "cpu",
		},
		{
			name:     "cuda lowercase",
			text:     "supports cuda acceleration",
			expected: "cuda",
		},
		{
			name:     "metal in help text",
			text:     "  --gpu-layers  N   offload layers to Metal GPU",
			expected: "metal",
		},
		{
			name:     "CUDA takes precedence over Metal when both present",
			text:     "Built with CUDA, Metal fallback",
			expected: "cuda",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectBackend(tt.text)
			if got != tt.expected {
				t.Errorf("DetectBackend(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestParseCapabilities(t *testing.T) {
	tests := []struct {
		name          string
		helpText      string
		wantFlash     bool
		wantNUMA      bool
		wantMLock     bool
		wantMMap      bool
		wantBackend   string
	}{
		{
			name: "all capabilities present with CUDA",
			helpText: `usage: llama-server [options]
  --flash-attn       enable flash attention
  --numa STRATEGY    set NUMA strategy
  --mlock            lock model in memory
  --mmap             memory-map the model
  Built with CUDA 12.4`,
			wantFlash:   true,
			wantNUMA:    true,
			wantMLock:   true,
			wantMMap:    true,
			wantBackend: "cuda",
		},
		{
			name: "no optional capabilities CPU only",
			helpText: `usage: llama-server [options]
  --model PATH       path to model file
  --threads N        number of threads`,
			wantFlash:   false,
			wantNUMA:    false,
			wantMLock:   false,
			wantMMap:    false,
			wantBackend: "cpu",
		},
		{
			name: "partial capabilities with Metal",
			helpText: `usage: llama-server [options]
  --flash-attn       enable flash attention
  --mmap             memory-map the model
  Metal acceleration enabled`,
			wantFlash:   true,
			wantNUMA:    false,
			wantMLock:   false,
			wantMMap:    true,
			wantBackend: "metal",
		},
		{
			name:        "empty help text",
			helpText:    "",
			wantFlash:   false,
			wantNUMA:    false,
			wantMLock:   false,
			wantMMap:    false,
			wantBackend: "cpu",
		},
		{
			name: "CUDA with compute capability",
			helpText: `llama-server --help
  --flash-attn    flash attention
  --numa          NUMA support
  Compiled for sm_100 CUDA`,
			wantFlash:   true,
			wantNUMA:    true,
			wantMLock:   false,
			wantMMap:    false,
			wantBackend: "cuda",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := &Capabilities{Backend: "cpu"}
			parseCapabilities(tt.helpText, caps)

			if caps.FlashAttention != tt.wantFlash {
				t.Errorf("FlashAttention = %v, want %v", caps.FlashAttention, tt.wantFlash)
			}
			if caps.NUMA != tt.wantNUMA {
				t.Errorf("NUMA = %v, want %v", caps.NUMA, tt.wantNUMA)
			}
			if caps.MLock != tt.wantMLock {
				t.Errorf("MLock = %v, want %v", caps.MLock, tt.wantMLock)
			}
			if caps.MMap != tt.wantMMap {
				t.Errorf("MMap = %v, want %v", caps.MMap, tt.wantMMap)
			}
			if caps.Backend != tt.wantBackend {
				t.Errorf("Backend = %q, want %q", caps.Backend, tt.wantBackend)
			}
		})
	}
}

func TestDetectCUDACompute(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "sm_100 present",
			text:     "Compiled for sm_100 CUDA",
			expected: "sm_100",
		},
		{
			name:     "sm_89 present",
			text:     "CUDA compute: sm_89",
			expected: "sm_89",
		},
		{
			name:     "no compute capability",
			text:     "compiled with CUDA support",
			expected: "",
		},
		{
			name:     "empty text",
			text:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCUDACompute(tt.text)
			if got != tt.expected {
				t.Errorf("detectCUDACompute(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestIsExecutable(t *testing.T) {
	// Non-existent path should return false.
	if isExecutable("/nonexistent/path/binary") {
		t.Error("isExecutable returned true for non-existent path")
	}
}
