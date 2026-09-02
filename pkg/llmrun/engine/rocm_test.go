package engine

import (
	"strings"
	"testing"
)

// A real ROCm llama.cpp build announces itself through the ggml-cuda code path,
// so its output carries BOTH "ROCm" and "CUDA". This is the exact text shape
// that made backend detection classify AMD builds as CUDA.
const rocmInitOutput = `ggml_cuda_init: GGML_CUDA_FORCE_MMQ:    no
ggml_cuda_init: GGML_CUDA_FORCE_CUBLAS: no
ggml_cuda_init: found 1 ROCm devices:
  Device 0: AMD Radeon 8060S Graphics, gfx1151 (0x1151), VMM: no, Wave Size: 32`

func TestDetectBackend_ROCm(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"ROCm device listing", rocmInitOutput, "rocm"},
		{"ROCm named alone", "built with ROCm 6.2", "rocm"},
		{"HIP runtime", "using HIP runtime 60241134", "rocm"},
		{"hipBLAS", "linked against hipBLAS", "rocm"},
		{"gfx target alone", "compiled for gfx942", "rocm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectBackend(c.text); got != c.want {
				t.Errorf("DetectBackend(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// The precedence is the whole point: a HIP build emits "CUDA" strings, so
// checking CUDA first would send every AMD box down the CUDA path.
func TestDetectBackend_ROCmBeatsCUDAWhenBothPresent(t *testing.T) {
	if got := DetectBackend(rocmInitOutput); got != "rocm" {
		t.Errorf("a ROCm build that also prints CUDA strings must detect as rocm, got %q", got)
	}
	// And the converse must still hold: a real CUDA build is untouched.
	cuda := "ggml_cuda_init: found 1 CUDA devices:\n  Device 0: NVIDIA GB10, compute capability 12.1"
	if got := DetectBackend(cuda); got != "cuda" {
		t.Errorf("a genuine CUDA build must still detect as cuda, got %q", got)
	}
}

// "HIP" appears inside ordinary English words. An unbounded substring match
// would classify a plain CPU build as ROCm.
func TestDetectBackend_NoFalsePositiveOnChipset(t *testing.T) {
	for _, text := range []string{
		"detected chipset: generic",
		"see the RELATIONSHIP between batch and ubatch",
		"shipping default weights",
	} {
		if got := DetectBackend(text); got != "cpu" {
			t.Errorf("DetectBackend(%q) = %q, want cpu (word-bounded HIP match)", text, got)
		}
	}
}

func TestDetectROCmArch(t *testing.T) {
	cases := []struct{ text, want string }{
		{rocmInitOutput, "gfx1151"},
		{"compiled for gfx90a", "gfx90a"},
		{"target GFX942", "gfx942"},
		{"no arch here", ""},
	}
	for _, c := range cases {
		if got := detectROCmArch(c.text); got != c.want {
			t.Errorf("detectROCmArch(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// parseCapabilities must fill the ROCm arch the same way it fills CUDA compute.
func TestParseCapabilities_ROCmArch(t *testing.T) {
	caps := &Capabilities{}
	parseCapabilities(rocmInitOutput, caps)
	if caps.Backend != "rocm" {
		t.Fatalf("backend = %q, want rocm", caps.Backend)
	}
	if caps.ROCmArch != "gfx1151" {
		t.Errorf("ROCmArch = %q, want gfx1151", caps.ROCmArch)
	}
	if caps.CUDACompute != "" {
		t.Errorf("a ROCm build must not report a CUDA compute capability, got %q", caps.CUDACompute)
	}
}

// The offload gate must accept a ROCm build. Before this, GPULayers=-1 on AMD
// hard-errored telling the operator to "Rebuild with CUDA".
func TestBuildCommand_ROCmAllowsGPUOffload(t *testing.T) {
	caps := Capabilities{
		Backend:    "rocm",
		ROCmArch:   "gfx1151",
		ServerMode: true,
		BinaryPath: "/opt/llama.cpp/bin/llama-server",
		BinaryDir:  "/opt/llama.cpp/bin",
	}
	cfg := RunConfig{ModelPath: "/models/m.gguf", GPULayers: -1, ServerMode: true}

	_, _, err := BuildCommand(cfg, caps)
	if err != nil {
		t.Fatalf("GPU offload on a ROCm build must be allowed, got error: %v", err)
	}
}

// A genuinely CPU-only build must still be refused, and the guidance must no
// longer name CUDA as the only remedy.
func TestBuildCommand_CPUOnlyStillRefusesOffload(t *testing.T) {
	caps := Capabilities{Backend: "cpu", ServerMode: true, BinaryPath: "/b/llama-server", BinaryDir: "/b"}
	cfg := RunConfig{ModelPath: "/models/m.gguf", GPULayers: -1, ServerMode: true}

	_, _, err := BuildCommand(cfg, caps)
	if err == nil {
		t.Fatal("a CPU-only build must still refuse GPU offload")
	}
	if got := err.Error(); !strings.Contains(got, "ROCm") {
		t.Errorf("error should offer ROCm as a remedy, got %q", got)
	}
}
