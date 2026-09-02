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

// Real `llama-server --list-devices` output from a ROCm build on gfx1151.
const rocmDeviceList = `Available devices:
  ROCm0: Radeon 8060S Graphics (64038 MiB, 64034 MiB free)
`

// Same build with no GPU visible: it answers, and the answer is "none".
const emptyDeviceList = `0.00.032.132 E ggml_cuda_init: failed to initialize ROCm: no ROCm-capable device is detected
Available devices:
  (none)
`

func TestDetectBackendFromDevices(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"rocm build with a device", rocmDeviceList, "rocm"},
		{"cuda build", "Available devices:\n  CUDA0: NVIDIA GB10 (131072 MiB, 130000 MiB free)\n", "cuda"},
		{"vulkan build", "Available devices:\n  Vulkan0: AMD Radeon (8192 MiB)\n", "vulkan"},
		{"no usable device", emptyDeviceList, ""},
		// A build too old for --list-devices prints usage or an error; that is
		// not a device listing and must not be read as one.
		{"unsupported flag", "error: unknown argument: --list-devices", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectBackendFromDevices(c.in); got != c.want {
				t.Errorf("DetectBackendFromDevices() = %q, want %q", got, c.want)
			}
		})
	}
}

// An empty listing is a real CPU-only answer; an unparseable one is a failed
// probe. Conflating them would either refuse offload on a GPU box or claim a
// GPU on a CPU box.
func TestDevicesListed(t *testing.T) {
	if !devicesListed(rocmDeviceList) || !devicesListed(emptyDeviceList) {
		t.Error("real listings must be recognized as listings")
	}
	if devicesListed("error: unknown argument: --list-devices") {
		t.Error("an unsupported-flag error must not count as a listing")
	}
}

// The regression that motivated all of this: a current llama-server --help is
// pure flag reference with no backend marker, and a successful GPU init prints
// nothing either. Text sniffing therefore reports cpu for a real GPU build,
// which then refuses GPU offload.
func TestDetectBackend_SilentGPUBuildLooksLikeCPU(t *testing.T) {
	quietVersion := "version: 0.3.0-dev (build 10752, commit b96806d96)\nbuilt with GNU 13.3.0 for Linux x86_64\n"
	if got := DetectBackend(quietVersion); got != "cpu" {
		t.Fatalf("precondition: quiet output should sniff as cpu, got %q", got)
	}
	// The device probe is what rescues it.
	if got := DetectBackendFromDevices(rocmDeviceList); got != "rocm" {
		t.Errorf("device listing must identify the backend the prose omits, got %q", got)
	}
}
