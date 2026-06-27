// Package emit renders a validated launch contract into a host-appropriate
// launch spec. It is the §2 render-target/driver seam: v1 ships three drivers
// (compose, docker run, podman quadlet) over one shared Spec, and a new target
// (native vLLM, TRT-LLM, remote) is another driver, not a rewrite. Emit is the
// END of the v1-A pipeline — it produces text for the operator (or today's
// compose+watchdog) to run. It deliberately does NOT launch, supervise, or own
// anything at runtime; that is v2 (B).
package emit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
)

// Host is the per-host launch context the contract knows nothing about: the
// engine image to run, the port to expose, and the volume mounts that make the
// model path inside the container resolve. These are render-time facts, supplied
// by the host driver, kept out of the contract so the same validated flags can
// be emitted for any host.
type Host struct {
	// Image is the engine container image (e.g. "vllm/vllm-openai:v0.23.0"). It
	// should match the contract key's engine digest; a mismatch is the operator's
	// to reconcile.
	Image string
	// Port is the host port mapped to the container's 8000. Defaults to 8000.
	Port int
	// Runtime is the container runtime label (e.g. "nvidia"). Defaults to "nvidia".
	Runtime string
	// Volumes maps host paths to container paths (read-only model mounts).
	Volumes []Mount
	// ServiceName is the compose/quadlet service name. Defaults to "vllm".
	ServiceName string
}

// Mount is a read-only host→container bind for model weights.
type Mount struct {
	Host      string
	Container string
}

func (h Host) port() int {
	if h.Port == 0 {
		return 8000
	}
	return h.Port
}

func (h Host) runtime() string {
	if h.Runtime == "" {
		return "nvidia"
	}
	return h.Runtime
}

func (h Host) service() string {
	if h.ServiceName == "" {
		return "vllm"
	}
	return h.ServiceName
}

// warningComment renders the contract's staleness warnings as leading comment
// lines so the operator sees them in the emitted artifact itself (the
// warn-not-gate posture: loud, in-band, but not blocking). Returns "" when there
// are no warnings.
func warningComment(r *contract.Resolved, prefix string) string {
	if len(r.Warnings) == 0 {
		return ""
	}
	var b strings.Builder
	for _, w := range r.Warnings {
		for line := range strings.SplitSeq(w, "\n") {
			b.WriteString(prefix)
			b.WriteString(" WARNING: ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// DockerRun renders a `docker run` invocation. Flags from the contract are
// emitted verbatim after the image; host facts (runtime, port, mounts) precede
// it. Long lines are continued for readability.
func DockerRun(r *contract.Resolved, h Host) string {
	var b strings.Builder
	b.WriteString(warningComment(r, "#"))
	b.WriteString("docker run -d \\\n")
	fmt.Fprintf(&b, "  --runtime %s --gpus all \\\n", h.runtime())
	b.WriteString("  --ipc host \\\n")
	fmt.Fprintf(&b, "  -p %d:8000 \\\n", h.port())
	for _, m := range h.Volumes {
		fmt.Fprintf(&b, "  -v %s:%s:ro \\\n", m.Host, m.Container)
	}
	fmt.Fprintf(&b, "  %s \\\n", h.Image)
	for i, f := range r.Flags {
		cont := " \\"
		if i == len(r.Flags)-1 {
			cont = ""
		}
		fmt.Fprintf(&b, "  %s%s\n", quoteArg(f), cont)
	}
	return b.String()
}

// Compose renders a docker-compose service definition mirroring the working
// vllm-config compose: nvidia runtime, GPU reservation, ipc host, the model
// mounts, and the validated flags as the command. The watchdog sidecar is NOT
// emitted — that is runtime supervision (v2 B); v1 emits only the engine service.
func Compose(r *contract.Resolved, h Host) string {
	var b strings.Builder
	b.WriteString(warningComment(r, "#"))
	b.WriteString("services:\n")
	fmt.Fprintf(&b, "  %s:\n", h.service())
	fmt.Fprintf(&b, "    image: %s\n", h.Image)
	fmt.Fprintf(&b, "    runtime: %s\n", h.runtime())
	b.WriteString("    restart: unless-stopped\n")
	b.WriteString("    deploy:\n      resources:\n        reservations:\n          devices:\n")
	b.WriteString("            - driver: nvidia\n              count: all\n              capabilities: [gpu]\n")
	b.WriteString("    ipc: host\n")
	b.WriteString("    ports:\n")
	fmt.Fprintf(&b, "      - \"%d:8000\"\n", h.port())
	if len(h.Volumes) > 0 {
		b.WriteString("    volumes:\n")
		for _, m := range h.Volumes {
			fmt.Fprintf(&b, "      - %s:%s:ro\n", m.Host, m.Container)
		}
	}
	b.WriteString("    command:\n")
	for _, f := range r.Flags {
		fmt.Fprintf(&b, "      - %s\n", yamlScalar(f))
	}
	return b.String()
}

// Quadlet renders a podman Quadlet .container unit. Same validated flags,
// expressed as a systemd-managed container — the future driver the seam table
// names, shipped in v1 to prove the seam is real, not theoretical.
func Quadlet(r *contract.Resolved, h Host) string {
	var b strings.Builder
	b.WriteString(warningComment(r, "#"))
	b.WriteString("[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", h.Image)
	fmt.Fprintf(&b, "PublishPort=%d:8000\n", h.port())
	b.WriteString("PodmanArgs=--ipc host --gpus all\n")
	for _, m := range h.Volumes {
		fmt.Fprintf(&b, "Volume=%s:%s:ro\n", m.Host, m.Container)
	}
	// Quadlet Exec is a single line carrying the full vLLM command.
	fmt.Fprintf(&b, "Exec=%s\n", quoteArgs(r.Flags))
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// Target names a render driver.
type Target string

const (
	TargetCompose   Target = "compose"
	TargetDockerRun Target = "docker-run"
	TargetQuadlet   Target = "quadlet"
)

// Targets returns the supported render targets in stable order.
func Targets() []Target {
	t := []Target{TargetCompose, TargetDockerRun, TargetQuadlet}
	slices.Sort(t)
	return t
}

// Render dispatches to the named driver. An unknown target is an error rather
// than a silent default, so a typo never emits the wrong format.
func Render(target Target, r *contract.Resolved, h Host) (string, error) {
	switch target {
	case TargetCompose:
		return Compose(r, h), nil
	case TargetDockerRun:
		return DockerRun(r, h), nil
	case TargetQuadlet:
		return Quadlet(r, h), nil
	default:
		return "", fmt.Errorf("unknown render target %q (have: compose, docker-run, quadlet)", target)
	}
}

// quoteArg shell-quotes a single argument when it contains characters the shell
// would split or interpret (so the JSON --default-chat-template-kwargs value
// survives a copy-paste into a terminal).
func quoteArg(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'{}$`\\|&;<>()*?[]#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteArgs joins flags into a single shell-safe command string.
func quoteArgs(flags []string) string {
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = quoteArg(f)
	}
	return strings.Join(parts, " ")
}

// yamlScalar renders a flag as a YAML scalar, quoting when needed so a value
// like a JSON object or a leading dash is not misread by the YAML parser.
func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":{}[]#&*!|>'\"%@`") || strings.HasPrefix(s, "-") || strings.Contains(s, ": ") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
