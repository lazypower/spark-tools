package servespec

import (
	"strings"
	"testing"

	"github.com/lazypower/spark-tools/internal/servecontract"
	"github.com/lazypower/spark-tools/internal/serving"
)

func resolvedFixture() *servecontract.Resolved {
	return &servecontract.Resolved{
		Key:   serving.ContractKey{Arch: "Qwen3VLForConditionalGeneration"},
		Flags: []string{"--model", "/models/m", "--enforce-eager"},
	}
}

func amdHost() Host {
	return Host{
		Image:       "kyuz0/vllm-therock-gfx1151:0.28.0+strix",
		Port:        8000,
		Accelerator: "amd:strix-halo:gfx1151",
		Volumes:     []Mount{{Host: "/var/data/hf", Container: "/models"}},
	}
}

func nvidiaHost() Host {
	return Host{
		Image:       "vllm/vllm-openai:v0.23.0",
		Port:        8000,
		Accelerator: "nvidia:gb10:sm121",
		Volumes:     []Mount{{Host: "/var/data/hf", Container: "/models"}},
	}
}

// ROCm has no runtime shim. The GPU arrives as the KFD compute node plus the
// DRM render nodes, so a spec that asks for "--runtime nvidia --gpus all"
// cannot start on an AMD box at all.
func TestDockerRun_AMDUsesDevicePassthrough(t *testing.T) {
	out := DockerRun(resolvedFixture(), amdHost())

	for _, want := range []string{"--device /dev/kfd", "--device /dev/dri", "--group-add video", "--group-add render"} {
		if !strings.Contains(out, want) {
			t.Errorf("AMD docker run missing %q\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"--runtime nvidia", "--gpus all"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("AMD docker run must not carry %q\n%s", unwanted, out)
		}
	}
}

func TestCompose_AMDUsesDevicesNotDeployReservation(t *testing.T) {
	out := Compose(resolvedFixture(), amdHost())

	for _, want := range []string{"devices:", "- /dev/kfd", "- /dev/dri", "group_add:"} {
		if !strings.Contains(out, want) {
			t.Errorf("AMD compose missing %q\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"driver: nvidia", "runtime: nvidia", "capabilities: [gpu]"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("AMD compose must not carry %q\n%s", unwanted, out)
		}
	}
}

func TestQuadlet_AMDUsesAddDeviceAndKeepGroups(t *testing.T) {
	out := Quadlet(resolvedFixture(), amdHost())

	for _, want := range []string{"AddDevice=/dev/kfd", "AddDevice=/dev/dri", "--group-add keep-groups"} {
		if !strings.Contains(out, want) {
			t.Errorf("AMD quadlet missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "--gpus all") {
		t.Errorf("AMD quadlet must not carry --gpus all\n%s", out)
	}
}

// The NVIDIA path must be untouched, including for a Host that names no
// accelerator at all -- that is every caller that predates this field.
func TestRenderers_NVIDIAUnchanged(t *testing.T) {
	explicit := nvidiaHost()
	unset := nvidiaHost()
	unset.Accelerator = ""

	for _, h := range []Host{explicit, unset} {
		run := DockerRun(resolvedFixture(), h)
		if !strings.Contains(run, "--runtime nvidia --gpus all") {
			t.Errorf("NVIDIA docker run must keep the runtime shim\n%s", run)
		}
		if strings.Contains(run, "/dev/kfd") {
			t.Errorf("NVIDIA docker run must not pass ROCm devices\n%s", run)
		}
		comp := Compose(resolvedFixture(), h)
		if !strings.Contains(comp, "driver: nvidia") {
			t.Errorf("NVIDIA compose must keep the device reservation\n%s", comp)
		}
		quad := Quadlet(resolvedFixture(), h)
		if !strings.Contains(quad, "PodmanArgs=--ipc host --gpus all") {
			t.Errorf("NVIDIA quadlet unchanged expected\n%s", quad)
		}
	}
}

// An unset accelerator must hash exactly as it did before the field existed, or
// every running NVIDIA instance is orphaned from its identity label.
func TestSpecHash_UnsetAcceleratorMatchesNVIDIA(t *testing.T) {
	unset := nvidiaHost()
	unset.Accelerator = ""

	if SpecHash(resolvedFixture(), unset) != SpecHash(resolvedFixture(), nvidiaHost()) {
		t.Error("an unset accelerator must hash identically to an explicit nvidia one")
	}
}

// An AMD spec is a materially different launch and must not collide with the
// NVIDIA spec it would otherwise share flags with.
func TestSpecHash_AMDDiffersFromNVIDIA(t *testing.T) {
	amd := amdHost()
	nv := amdHost()
	nv.Accelerator = "nvidia:gb10:sm121"

	if SpecHash(resolvedFixture(), amd) == SpecHash(resolvedFixture(), nv) {
		t.Error("AMD and NVIDIA specs with identical flags must not share a spec hash")
	}
}

func TestGPUVendor(t *testing.T) {
	cases := []struct{ accel, want string }{
		{"amd:strix-halo:gfx1151", "amd"},
		{"nvidia:gb10:sm121", "nvidia"},
		{"", "nvidia"},
		{"malformed", "nvidia"},
	}
	for _, c := range cases {
		if got := (Host{Accelerator: c.accel}).gpuVendor(); got != c.want {
			t.Errorf("gpuVendor(%q) = %q, want %q", c.accel, got, c.want)
		}
	}
}
