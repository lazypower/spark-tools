package servespec

import (
	"encoding/json"

	"github.com/lazypower/spark-tools/internal/servecontract"
)

// PodmanSpec is a machine-readable launch spec: everything the podman runtime
// driver needs to start the container, with no shell in the loop.
//
// The other targets render text for a human or another tool to interpret --
// compose YAML, a shell command line, a systemd unit. Those are the wrong shape
// for a driver that has to APPLY the spec: it would have to parse back out what
// the renderer just formatted, and on this class of host there may be no compose
// implementation to hand the YAML to at all. So the driver gets structure.
type PodmanSpec struct {
	// Engine is the CLI that runs it ("podman"), recorded so a spec on disk says
	// what produced it.
	Engine string `json:"engine"`
	// Name is the container name; the driver also uses it for the project label.
	Name string `json:"name"`
	// Image is the engine container image.
	Image string `json:"image"`
	// Args are the validated launch flags, including any command prefix.
	Args []string `json:"args"`
	// Devices are host device nodes to pass through (AMD needs /dev/kfd and
	// /dev/dri; NVIDIA uses a runtime shim instead and leaves this empty).
	Devices []string `json:"devices,omitempty"`
	// GroupAdd carries supplementary group handling (keep-groups on rootless
	// podman with an AMD GPU).
	GroupAdd []string `json:"groupAdd,omitempty"`
	// Runtime is the container runtime shim, when the vendor needs one.
	Runtime string `json:"runtime,omitempty"`
	// AllGPUs requests every GPU (the NVIDIA --gpus all form).
	AllGPUs bool `json:"allGpus,omitempty"`
	// IPCHost maps the host IPC namespace; vLLM workers need the shared memory.
	IPCHost bool `json:"ipcHost"`
	// Port is the host port mapped to the container's 8000.
	Port int `json:"port"`
	// Volumes are read-only model mounts.
	Volumes []Mount `json:"volumes,omitempty"`
	// Labels are the identity labels reconcile verifies.
	Labels map[string]string `json:"labels,omitempty"`
	// Watchdog, when set, is the wedge-detection sidecar. Reconcile requires it
	// to call an instance serving, so a driver that omits it produces a stack
	// that can never reach serving no matter how healthy the engine is.
	Watchdog *PodmanWatchdog `json:"watchdog,omitempty"`
	// Warnings are the render-time warnings the text targets emit as comments.
	// JSON has nowhere to put a comment, and dropping them would make this the
	// one target that silently loses the staleness and GPU-access notices.
	Warnings []string `json:"warnings,omitempty"`
}

// watchdogImage needs a container CLI on board, because wedge detection works by
// restarting the engine container through the host engine's socket. The compose
// target uses docker:cli for the same reason; the podman equivalent is the
// upstream podman image.
const watchdogImage = "quay.io/podman/stable"

// PodmanJSON renders the machine-readable spec the podman driver applies.
func PodmanJSON(r *servecontract.Resolved, h Host) string {
	flags, warnings := planLaunch(r, h)
	warnings = append(warnings, h.amdKeepGroupsWarning()...)

	spec := PodmanSpec{
		Engine:   enginePodman,
		Name:     h.service(),
		Image:    h.Image,
		Args:     flags,
		IPCHost:  true,
		Port:     h.port(),
		Volumes:  h.Volumes,
		Labels:   h.Labels,
		Warnings: warnings,
	}

	if h.Watchdog != nil {
		spec.Watchdog = &PodmanWatchdog{
			Image:         watchdogImage,
			ScriptHostDir: h.Watchdog.ScriptHostDir,
			Project:       h.Watchdog.Project,
			Service:       spec.ServiceName(),
		}
	}

	if h.gpuVendor() == vendorAMD {
		spec.Devices = []string{"/dev/kfd", "/dev/dri"}
		// keep-groups is the rootless-podman form; naming groups does not work
		// there because host GIDs cannot be mapped into the user namespace.
		spec.GroupAdd = []string{"keep-groups"}
	} else {
		spec.Runtime = h.runtime()
		spec.AllGPUs = true
	}

	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		// The struct is plain data; a marshal failure is not reachable, but
		// returning empty beats panicking inside a renderer.
		return ""
	}
	return string(b) + "\n"
}

// PodmanWatchdog describes the sidecar that restarts a wedged engine.
type PodmanWatchdog struct {
	// Image must carry a container CLI, since the watchdog restarts the engine
	// by talking to the host engine's socket.
	Image string `json:"image"`
	// ScriptHostDir holds watchdog.sh, mounted read-only at /watchdog.
	ScriptHostDir string `json:"scriptHostDir"`
	// Project and Service identify the container to restart, by label.
	Project string `json:"project"`
	Service string `json:"service"`
}

// ServiceName is the engine service this spec launches, as reconcile knows it.
func (s PodmanSpec) ServiceName() string {
	if s.Name == "" {
		return "vllm"
	}
	return s.Name
}

// ParsePodmanSpec reads a spec written by PodmanJSON.
func ParsePodmanSpec(data []byte) (PodmanSpec, error) {
	var s PodmanSpec
	err := json.Unmarshal(data, &s)
	return s, err
}
