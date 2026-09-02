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

// The Docker group form silently yields a GPU-less container under rootless
// podman, so an AMD spec that renders it must say so where the operator will
// see it: in the spec itself.
func TestDockerRun_AMDWarnsAboutRootlessPodmanGroups(t *testing.T) {
	out := DockerRun(resolvedFixture(), amdHost())
	if !strings.Contains(out, "keep-groups") {
		t.Errorf("AMD docker run must warn that rootless podman needs keep-groups\n%s", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Errorf("the note must render as a warning comment\n%s", out)
	}
	// The wording carries the load here. An operator who reads "may need
	// adjustment" skips it; one who reads that the spec starts successfully
	// with no GPU does not. The failure being SILENT is the whole point.
	if !strings.Contains(out, "STARTS SUCCESSFULLY AND RUNS THE ENGINE WITH NO GPU") {
		t.Errorf("the warning must state that the failure is silent, not merely that the form differs\n%s", out)
	}
}

func TestCompose_AMDWarnsAboutRootlessPodmanGroups(t *testing.T) {
	out := Compose(resolvedFixture(), amdHost())
	if !strings.Contains(out, "keep-groups") {
		t.Errorf("AMD compose must warn about the podman group form\n%s", out)
	}
}

// Quadlet is podman-native and already emits keep-groups, so the group warning
// would be noise about a problem it does not have. It may still carry OTHER
// warnings (an uncovered mount, staleness), so this asserts only that the group
// advice is absent -- not that the spec is warning-free.
func TestQuadlet_AMDDoesNotCarryGroupWarning(t *testing.T) {
	out := Quadlet(resolvedFixture(), amdHost())
	if strings.Contains(out, "keep-groups there") {
		t.Errorf("quadlet already emits keep-groups and must not be told to switch to it\n%s", out)
	}
}

// NVIDIA uses a runtime shim and no group juggling; the warning must not leak.
func TestRenderers_NVIDIANoGroupWarning(t *testing.T) {
	for _, render := range []func(*servecontract.Resolved, Host) string{DockerRun, Compose, Quadlet} {
		if out := render(resolvedFixture(), nvidiaHost()); strings.Contains(out, "keep-groups") {
			t.Errorf("NVIDIA specs must not mention keep-groups\n%s", out)
		}
	}
}

// --- container engine ---

func podmanHost() Host {
	h := amdHost()
	h.ContainerEngine = "podman"
	return h
}

// Rootless podman needs keep-groups; naming groups there yields a GPU-less
// container. A spec rendered FOR podman must carry the form that works.
func TestDockerRun_PodmanRendersKeepGroups(t *testing.T) {
	out := DockerRun(resolvedFixture(), podmanHost())

	if !strings.Contains(out, "--group-add keep-groups") {
		t.Errorf("podman spec must use keep-groups\n%s", out)
	}
	if strings.Contains(out, "--group-add video") {
		t.Errorf("podman spec must not name groups\n%s", out)
	}
	// The command word must match the flags, or the line is not runnable as
	// printed: docker rejects keep-groups.
	if !strings.HasPrefix(strings.TrimSpace(stripComments(out)), "podman run -d") {
		t.Errorf("podman spec must invoke podman, not docker\n%s", out)
	}
	// A spec that already carries the working form has nothing to warn about.
	if strings.Contains(out, "STARTS SUCCESSFULLY AND RUNS THE ENGINE WITH NO GPU") {
		t.Errorf("a podman-rendered spec must not carry the docker-form warning\n%s", out)
	}
}

func TestCompose_PodmanRendersKeepGroups(t *testing.T) {
	out := Compose(resolvedFixture(), podmanHost())
	if !strings.Contains(out, "- keep-groups") {
		t.Errorf("podman compose must use keep-groups\n%s", out)
	}
	if strings.Contains(out, "- video") {
		t.Errorf("podman compose must not name groups\n%s", out)
	}
}

// An unset engine must render exactly what it always did.
func TestDockerRun_UnsetEngineIsDocker(t *testing.T) {
	out := DockerRun(resolvedFixture(), amdHost())
	if !strings.Contains(out, "--group-add video --group-add render") {
		t.Errorf("unset engine must keep the historical docker form\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(stripComments(out)), "docker run -d") {
		t.Errorf("unset engine must invoke docker\n%s", out)
	}
}

// stripComments drops the leading warning-comment block so the first real line
// can be asserted.
func stripComments(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// --- engine command prefix ---

// The official vllm image sets ENTRYPOINT ["vllm","serve"], but an image with an
// empty entrypoint makes the runtime try to exec "--model" as a program. The
// prefix is what makes such an image launchable.
func TestDockerRun_CommandPrefixPrecedesFlags(t *testing.T) {
	h := podmanHost()
	h.Command = []string{"vllm", "serve"}
	out := stripComments(DockerRun(resolvedFixture(), h))

	img := strings.Index(out, h.Image)
	vllm := strings.Index(out, "vllm")
	model := strings.Index(out, "--model")
	if img < 0 || vllm < 0 || model < 0 {
		t.Fatalf("expected image, command and flags in output\n%s", out)
	}
	if !(img < vllm && vllm < model) {
		t.Errorf("command prefix must sit between the image and the flags (img=%d cmd=%d model=%d)\n%s", img, vllm, model, out)
	}
}

// Empty Command must leave the official-image path untouched, hash included.
func TestSpecHash_EmptyCommandUnchanged(t *testing.T) {
	withEmpty := nvidiaHost()
	withNil := nvidiaHost()
	withNil.Command = nil
	if SpecHash(resolvedFixture(), withEmpty) != SpecHash(resolvedFixture(), withNil) {
		t.Error("an empty command prefix must not change the spec hash")
	}
}

// A command prefix changes what actually launches, so it must change identity.
func TestSpecHash_CommandPrefixChangesHash(t *testing.T) {
	base := podmanHost()
	pref := podmanHost()
	pref.Command = []string{"vllm", "serve"}
	if SpecHash(resolvedFixture(), base) == SpecHash(resolvedFixture(), pref) {
		t.Error("a command prefix must produce a distinct spec hash")
	}
}

func TestQuadlet_CommandPrefixInExec(t *testing.T) {
	h := podmanHost()
	h.Command = []string{"vllm", "serve"}
	out := Quadlet(resolvedFixture(), h)
	if !strings.Contains(out, "Exec=vllm serve --model") {
		t.Errorf("quadlet Exec must start with the command prefix\n%s", out)
	}
}
