package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/lazypower/spark-tools/internal/servespec"
)

// Podman drives the host with plain `podman run`, applying the machine-readable
// spec that servespec.TargetPodman renders.
//
// It exists because the compose driver cannot run on an appliance host: there
// may be no compose implementation at all (podman is daemonless and ships none;
// the docker CLI without a daemon rejects the subcommand and prints a usage
// dump about an unknown -p flag). Rather than add a host dependency to satisfy
// a driver, this speaks the container engine that is actually installed.
//
// Quadlet was the other candidate and is deliberately not used here: a systemd
// unit is launched by the user manager, whose credentials are fixed at session
// start, so --group-add keep-groups inherits nothing and the container comes up
// with no GPU. A driver invoked from the CLI inherits the caller's groups, which
// is the behavior an operator expects.
type Podman struct {
	// Bin is the podman executable; empty means "podman" on PATH.
	Bin string
}

// NewPodman returns a driver using podman from PATH.
func NewPodman() *Podman { return &Podman{} }

func (p *Podman) bin() string {
	if p.Bin == "" {
		return "podman"
	}
	return p.Bin
}

// projectLabel is how a container is tied back to its managed instance. Compose
// gets this for free from its own project label; driving podman directly means
// stamping it explicitly.
const projectLabel = "llm-serve.project"

// composeServiceLabel is the label reconcile reads to tell services apart. It is
// named for compose because that is where it originated, but it is the shared
// vocabulary of the reconcile predicate, not a compose implementation detail —
// so a non-compose driver has to speak it too.
const composeServiceLabel = "com.docker.compose.service"

// Up applies the spec and starts the stack detached. It does not wait for
// readiness — that is the Prober's job, polled by lifecycle.
//
// The containers go in a POD so the watchdog and the engine share a network
// namespace. Compose gets that for free from its project network; driving
// podman directly does not, and the watchdog has to reach the engine's /metrics
// to see token progress at all. A pod also means one port publish and one
// teardown command for the whole stack.
func (p *Podman) Up(ctx context.Context, projectName, specPath string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}
	spec, err := servespec.ParsePodmanSpec(data)
	if err != nil {
		return fmt.Errorf("parsing spec %s: %w", specPath, err)
	}

	// Start from a clean pod: a previous failed bring-up may have left one, and
	// refusing to start because of it turns a recoverable state into manual
	// cleanup.
	if err := p.podRemove(ctx, projectName); err != nil {
		return err
	}
	podArgs := []string{"pod", "create", "--name", projectName}
	if spec.Port > 0 {
		podArgs = append(podArgs, "-p", fmt.Sprintf("%d:8000", spec.Port))
	}
	if out, err := exec.CommandContext(ctx, p.bin(), podArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("podman pod create: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if err := p.runEngine(ctx, projectName, spec); err != nil {
		return err
	}
	if spec.Watchdog != nil {
		if err := p.runWatchdog(ctx, projectName, spec); err != nil {
			return err
		}
	}
	return nil
}

// runEngine starts the vLLM container inside the pod.
func (p *Podman) runEngine(ctx context.Context, projectName string, spec servespec.PodmanSpec) error {
	args := []string{"run", "-d", "--replace", "--pod", projectName,
		"--name", projectName + "-" + spec.ServiceName(),
	}
	args = append(args, p.identityArgs(projectName, spec.ServiceName(), spec.Labels)...)
	for _, d := range spec.Devices {
		args = append(args, "--device", d)
	}
	for _, g := range spec.GroupAdd {
		args = append(args, "--group-add", g)
	}
	if spec.Runtime != "" {
		args = append(args, "--runtime", spec.Runtime)
	}
	if spec.AllGPUs {
		args = append(args, "--gpus", "all")
	}
	if spec.IPCHost {
		args = append(args, "--ipc", "host")
	}
	for _, m := range spec.Volumes {
		// :z relabels for SELinux, which is enforcing on this class of host and
		// otherwise denies the container access to the model mount.
		args = append(args, "-v", m.Host+":"+m.Container+":ro,z")
	}
	args = append(args, spec.Image)
	args = append(args, spec.Args...)

	if out, err := exec.CommandContext(ctx, p.bin(), args...).CombinedOutput(); err != nil {
		return fmt.Errorf("podman run (engine): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runWatchdog starts the wedge-detection sidecar inside the same pod.
//
// It restarts the engine by talking to the HOST podman through its socket, so
// the socket is mounted and CONTAINER_HOST points at it. Because both
// containers share the pod's network namespace, the engine is reachable at
// localhost rather than by service name.
func (p *Podman) runWatchdog(ctx context.Context, projectName string, spec servespec.PodmanSpec) error {
	w := spec.Watchdog
	sock := fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid())
	args := []string{"run", "-d", "--replace", "--pod", projectName,
		"--name", projectName + "-watchdog",
	}
	args = append(args, p.identityArgs(projectName, "watchdog", spec.Labels)...)
	args = append(args,
		"-v", w.ScriptHostDir+":/watchdog:ro,z",
		"-v", sock+":/run/podman/podman.sock",
		"-e", "CONTAINER_HOST=unix:///run/podman/podman.sock",
		"-e", "CTR=podman",
		"-e", "VLLM_URL=http://localhost:8000",
		"-e", "COMPOSE_PROJECT="+w.Project,
		"-e", "COMPOSE_SERVICE="+w.Service,
		"--entrypoint", "/bin/sh",
		w.Image, "/watchdog/watchdog.sh",
	)
	if out, err := exec.CommandContext(ctx, p.bin(), args...).CombinedOutput(); err != nil {
		return fmt.Errorf("podman run (watchdog): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// identityArgs stamps the labels reconcile verifies: the instance identity, the
// managed-by marker, the project, and which service this container is.
func (p *Podman) identityArgs(projectName, service string, labels map[string]string) []string {
	args := []string{
		"--label", projectLabel + "=" + projectName,
		"--label", managedByLabel,
		"--label", composeServiceLabel + "=" + service,
	}
	for k, v := range labels {
		args = append(args, "--label", k+"="+v)
	}
	return args
}

// podRemove tears the pod down, tolerating one that is not there.
func (p *Podman) podRemove(ctx context.Context, projectName string) error {
	out, err := exec.CommandContext(ctx, p.bin(), "pod", "rm", "-f", "--ignore", projectName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman pod rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Down removes the pod and everything in it. It must be idempotent, and a
// teardown it cannot CONFIRM must be an error so lifecycle keeps the recovery
// handle.
func (p *Podman) Down(ctx context.Context, projectName, specPath string) error {
	if err := p.podRemove(ctx, projectName); err != nil {
		return err
	}
	// Confirm rather than trust the exit code: --ignore succeeds for a pod that
	// was never there, which is the same status as one genuinely removed, and
	// the fail-closed contract needs to tell those apart.
	ids, err := p.projectIDs(ctx, projectName)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return fmt.Errorf("podman pod rm did not remove %s (%d container(s) remain)", projectName, len(ids))
	}
	return nil
}

// Inspect reports the actual runtime state of the project's container.
func (p *Podman) Inspect(ctx context.Context, projectName, specPath string) (RuntimeState, error) {
	ids, err := p.projectIDs(ctx, projectName)
	if err != nil {
		return RuntimeState{}, err
	}
	if len(ids) == 0 {
		return RuntimeState{Exists: false}, nil
	}
	services, err := inspectContainers(ctx, p.bin(), ids)
	if err != nil {
		return RuntimeState{}, err
	}
	return RuntimeState{Exists: true, Services: services}, nil
}

// ListRunning reports every RUNNING container on the host, with labels and
// bind-mount sources, so eviction protection is reality-based and also covers
// containers llm-serve did not launch.
func (p *Podman) ListRunning(ctx context.Context) ([]ServiceState, error) {
	out, err := exec.CommandContext(ctx, p.bin(), "ps", "-q").Output()
	if err != nil {
		return nil, fmt.Errorf("podman ps (running): %w", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return nil, nil
	}
	return inspectContainers(ctx, p.bin(), ids)
}

// projectIDs returns the IDs of llm-serve-managed containers for a project,
// running or not.
func (p *Podman) projectIDs(ctx context.Context, projectName string) ([]string, error) {
	out, err := exec.CommandContext(ctx, p.bin(), "ps", "-aq",
		"--filter", "label="+projectLabel+"="+projectName,
		"--filter", "label="+managedByLabel).Output()
	if err != nil {
		return nil, fmt.Errorf("podman ps: %w", err)
	}
	return strings.Fields(strings.TrimSpace(string(out))), nil
}

// compile-time proof the driver satisfies the seam the lifecycle reconciles
// against.
var _ Runtime = (*Podman)(nil)

// DetectEngine reports which container engine this host actually uses, by
// PRESENCE and without executing either binary.
//
// Probing a daemon would be worse than useless here: docker.socket is
// socket-activated on a CoreOS-style host, so a `docker info` from a caller who
// can reach the socket STARTS a daemon the operator chose not to run, and then
// reports docker as available because it just made it so.
//
// Preferring podman when both exist is the safe asymmetry. Choosing podman on a
// docker host fails loudly (podman is not there, or the container plainly does
// not appear in `docker ps`); choosing docker on a podman host fails by leaving
// the lifecycle unable to see its own containers, which surfaces as liveness
// protecting everything and teardown never confirming.
func DetectEngine() string {
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	return "docker"
}

// For returns the driver for a named engine, defaulting to compose for anything
// that is not podman so existing docker hosts are untouched.
func For(engine string) Runtime {
	if engine == "podman" {
		return NewPodman()
	}
	return NewCompose()
}
