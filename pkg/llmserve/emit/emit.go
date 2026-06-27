// Package emit renders a validated launch contract into a host-appropriate
// launch spec. It is the §2 render-target/driver seam: v1 ships three drivers
// (compose, docker run, podman quadlet) over one shared Spec, and a new target
// (native vLLM, TRT-LLM, remote) is another driver, not a rewrite. Emit is the
// END of the v1-A pipeline — it produces text for the operator (or today's
// compose+watchdog) to run. It deliberately does NOT launch, supervise, or own
// anything at runtime; that is v2 (B).
package emit

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
)

// WatchdogScript is the engine-wedge watchdog (running>0 & token-counter flat —
// invisible to /health). It is embedded so the emitted stack is self-contained;
// the lifecycle layer writes it to the host dir the watchdog service mounts.
//
//go:embed watchdog.sh
var WatchdogScript string

// Watchdog configures the wedge-detection sidecar B1 emits alongside the engine,
// preserving the production failure mode `restart: unless-stopped` cannot catch.
// nil means no sidecar — but the lifecycle serving predicate requires it, so a
// serving instance must supply one (fail-closed by design).
type Watchdog struct {
	// ScriptHostDir is the host directory holding watchdog.sh, mounted read-only
	// at /watchdog in the sidecar.
	ScriptHostDir string
	// Project is the compose project name, so the sidecar can target the engine
	// container by label to restart it when wedged.
	Project string
}

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
	// Labels are immutable identity labels stamped on every emitted service, so a
	// lifecycle reconcile can prove the running stack IS a specific managed
	// instance (B1). Rendered in sorted key order so the emitted spec — and thus
	// its content hash — is deterministic.
	Labels map[string]string
	// Watchdog, when set, emits the wedge-detection sidecar (compose driver only).
	Watchdog *Watchdog
}

// sortedLabels returns the host labels as ordered key=value pairs (sorted by key)
// so emit output is deterministic regardless of map iteration order.
func (h Host) sortedLabels() []string {
	if len(h.Labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h.Labels))
	for k := range h.Labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k + "=" + h.Labels[k]
	}
	return out
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

// planLaunch specializes the validated flags for a concrete host — the per-host
// driver's core job. The contract emits --model with the artifact's HOST path
// (it knows nothing about mounts); planLaunch rewrites it to the path the model
// resolves to INSIDE the container via the volume mounts, and serves that
// container path as an additional model name so callers that address the model
// by path keep resolving (run.sh parity: alias + container path). It returns the
// host-ready flags and the combined warnings (the contract's staleness warning
// plus any host-specialization warning, e.g. a model not covered by a mount —
// which would make the emitted spec fail to find the model at runtime).
func planLaunch(r *contract.Resolved, h Host) (flags []string, warnings []string) {
	flags = slices.Clone(r.Flags)
	warnings = append(warnings, r.Warnings...)

	mi := flagIndex(flags, "--model")
	if mi < 0 || mi+1 >= len(flags) {
		return flags, warnings
	}
	hostPath := flags[mi+1]
	cp, ok := containerPath(hostPath, h.Volumes)
	if !ok {
		warnings = append(warnings, fmt.Sprintf(
			"--model %s is not covered by any volume mount; the container will not find the model — add a matching --mount <hostdir>:<containerdir>",
			hostPath))
		return flags, warnings
	}
	flags[mi+1] = cp

	// Serve the container path as an additional --served-model-name (run.sh
	// parity), so scripts that address the model by its /models/... path resolve.
	if si := flagIndex(flags, "--served-model-name"); si >= 0 && si+1 < len(flags) && flags[si+1] != cp {
		flags = slices.Insert(flags, si+2, cp)
	}
	return flags, warnings
}

// flagIndex returns the position of a flag token, or -1.
func flagIndex(flags []string, name string) int {
	return slices.Index(flags, name)
}

// containerPath maps a host path to the path it is bound to inside the container
// via the volume mounts. Relative mount host paths are resolved against the
// current directory (where compose/run is invoked from, as run.sh does). Returns
// ok=false when no mount covers the path — the caller turns that into a loud
// warning rather than emitting a spec that silently can't find its model.
func containerPath(hostPath string, mounts []Mount) (string, bool) {
	absModel, err := filepath.Abs(hostPath)
	if err != nil {
		return "", false
	}
	for _, m := range mounts {
		absHost, err := filepath.Abs(m.Host)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absHost, absModel)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // model is not under this mount
		}
		if rel == "." {
			return m.Container, true
		}
		return path.Join(m.Container, filepath.ToSlash(rel)), true
	}
	return "", false
}

// warningComment renders warnings as leading comment lines so the operator sees
// them in the emitted artifact itself (warn-not-gate: loud, in-band, not
// blocking). Returns "" when there are none.
func warningComment(warnings []string, prefix string) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	for _, w := range warnings {
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
	flags, warnings := planLaunch(r, h)
	var b strings.Builder
	b.WriteString(warningComment(warnings, "#"))
	b.WriteString("docker run -d \\\n")
	fmt.Fprintf(&b, "  --runtime %s --gpus all \\\n", h.runtime())
	b.WriteString("  --ipc host \\\n")
	fmt.Fprintf(&b, "  -p %d:8000 \\\n", h.port())
	for _, m := range h.Volumes {
		fmt.Fprintf(&b, "  -v %s:%s:ro \\\n", m.Host, m.Container)
	}
	for _, l := range h.sortedLabels() {
		fmt.Fprintf(&b, "  --label %s \\\n", quoteArg(l))
	}
	fmt.Fprintf(&b, "  %s \\\n", h.Image)
	for i, f := range flags {
		cont := " \\"
		if i == len(flags)-1 {
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
	flags, warnings := planLaunch(r, h)
	var b strings.Builder
	b.WriteString(warningComment(warnings, "#"))
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
	if labels := h.sortedLabels(); len(labels) > 0 {
		b.WriteString("    labels:\n")
		for _, l := range labels {
			fmt.Fprintf(&b, "      - %s\n", yamlScalar(l))
		}
	}
	b.WriteString("    command:\n")
	for _, f := range flags {
		// Every command entry MUST render as a YAML string. A bare numeric value
		// (e.g. --max-model-len 131072) is otherwise parsed as an int and docker
		// compose rejects it ("command.N must be a string"). Always-quote — a
		// lenient Go YAML parser coerces ints to strings (so a round-trip test
		// misses this), but compose does not.
		fmt.Fprintf(&b, "      - %s\n", yamlQuoted(f))
	}

	if h.Watchdog != nil {
		writeWatchdogService(&b, h)
	}
	return b.String()
}

// writeWatchdogService renders the wedge-detection sidecar, carrying the SAME
// identity labels as the engine (so reconcile sees it as part of this instance)
// plus its own compose-service label. It targets the engine container by the
// compose project/service labels to restart it when the token counter goes flat.
func writeWatchdogService(b *strings.Builder, h Host) {
	engine := h.service()
	b.WriteString("  watchdog:\n")
	b.WriteString("    image: docker:cli\n")
	b.WriteString("    restart: unless-stopped\n")
	fmt.Fprintf(b, "    depends_on:\n      - %s\n", engine)
	b.WriteString("    volumes:\n")
	b.WriteString("      - /var/run/docker.sock:/var/run/docker.sock\n")
	fmt.Fprintf(b, "      - %s:/watchdog:ro\n", h.Watchdog.ScriptHostDir)
	if labels := h.sortedLabels(); len(labels) > 0 {
		b.WriteString("    labels:\n")
		for _, l := range labels {
			fmt.Fprintf(b, "      - %s\n", yamlScalar(l))
		}
	}
	b.WriteString("    environment:\n")
	fmt.Fprintf(b, "      - VLLM_URL=http://%s:8000\n", engine)
	fmt.Fprintf(b, "      - COMPOSE_PROJECT=%s\n", h.Watchdog.Project)
	fmt.Fprintf(b, "      - COMPOSE_SERVICE=%s\n", engine)
	b.WriteString("    entrypoint: [\"/bin/sh\", \"/watchdog/watchdog.sh\"]\n")
}

// Quadlet renders a podman Quadlet .container unit. Same validated flags,
// expressed as a systemd-managed container — the future driver the seam table
// names, shipped in v1 to prove the seam is real, not theoretical.
func Quadlet(r *contract.Resolved, h Host) string {
	flags, warnings := planLaunch(r, h)
	var b strings.Builder
	b.WriteString(warningComment(warnings, "#"))
	b.WriteString("[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", h.Image)
	fmt.Fprintf(&b, "PublishPort=%d:8000\n", h.port())
	b.WriteString("PodmanArgs=--ipc host --gpus all\n")
	for _, m := range h.Volumes {
		fmt.Fprintf(&b, "Volume=%s:%s:ro\n", m.Host, m.Container)
	}
	for _, l := range h.sortedLabels() {
		fmt.Fprintf(&b, "Label=%s\n", l)
	}
	// Quadlet Exec is a single line carrying the full vLLM command.
	fmt.Fprintf(&b, "Exec=%s\n", quoteArgs(flags))
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

// SpecHash is a stable content hash of the runtime-identity-relevant inputs: the
// host-specialized command, the image, the port, and the mounts. It deliberately
// EXCLUDES Host.Labels — the spec-hash is itself one of those labels, so hashing
// the labels would be circular. Reconcile compares this against the running
// container's spec-hash label to prove it came from this exact spec.
func SpecHash(r *contract.Resolved, h Host) string {
	flags, _ := planLaunch(r, h)
	var b strings.Builder
	for _, f := range flags {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "image=%s\nport=%d\n", h.Image, h.port())
	mounts := make([]string, 0, len(h.Volumes))
	for _, m := range h.Volumes {
		mounts = append(mounts, m.Host+":"+m.Container)
	}
	slices.Sort(mounts)
	for _, m := range mounts {
		fmt.Fprintf(&b, "mount=%s\n", m)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
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

// yamlQuoted always renders s as a YAML double-quoted scalar, forcing the string
// type. Used for command entries, which docker compose requires to be strings —
// a bare numeric/bool/null token would be coerced to the wrong type.
func yamlQuoted(s string) string {
	r := strings.ReplaceAll(s, `\`, `\\`)
	r = strings.ReplaceAll(r, `"`, `\"`)
	return `"` + r + `"`
}
