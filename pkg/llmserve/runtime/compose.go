package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// managedByLabel is the label every emitted stack carries; Inspect filters on it
// plus the instance label so it only ever observes llm-serve-managed containers.
const managedByLabel = "managed-by=llm-serve"

// Compose drives the host via `docker compose` (drive-the-driver). It is exec-only
// and stateless; its correctness is verified on the host, not in build CI.
type Compose struct {
	// Bin is the base command; defaults to {"docker","compose"}. Overridable for
	// podman-compose or a pinned path.
	Bin []string
}

// NewCompose returns a Compose driver using `docker compose`.
func NewCompose() *Compose { return &Compose{Bin: []string{"docker", "compose"}} }

func (c *Compose) bin() []string {
	if len(c.Bin) == 0 {
		return []string{"docker", "compose"}
	}
	return c.Bin
}

// args builds a `docker compose -p <proj> -f <spec> <rest...>` invocation.
func (c *Compose) args(projectName, specPath string, rest ...string) []string {
	a := append([]string{}, c.bin()[1:]...)
	a = append(a, "-p", projectName, "-f", specPath)
	return append(a, rest...)
}

func (c *Compose) run(ctx context.Context, projectName, specPath string, rest ...string) ([]byte, error) {
	args := c.args(projectName, specPath, rest...)
	cmd := exec.CommandContext(ctx, c.bin()[0], args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("compose %s: %w: %s", strings.Join(rest, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Up applies the spec and starts the stack detached.
func (c *Compose) Up(ctx context.Context, projectName, specPath string) error {
	_, err := c.run(ctx, projectName, specPath, "up", "-d")
	return err
}

// Down stops and removes the stack. `compose down` is idempotent; a non-zero exit
// means we could NOT confirm teardown, which surfaces as an error so lifecycle
// keeps the recovery handle.
func (c *Compose) Down(ctx context.Context, projectName, specPath string) error {
	_, err := c.run(ctx, projectName, specPath, "down")
	return err
}

// Inspect lists the project's containers and their labels via `docker inspect`,
// filtered to llm-serve-managed containers. It addresses containers by the docker
// engine (not compose ps) so it can read the full label set reconcile verifies.
func (c *Compose) Inspect(ctx context.Context, projectName, specPath string) (RuntimeState, error) {
	// Container IDs for this project that we manage.
	idsOut, err := c.dockerPS(ctx, projectName)
	if err != nil {
		return RuntimeState{}, err
	}
	ids := strings.Fields(strings.TrimSpace(string(idsOut)))
	if len(ids) == 0 {
		return RuntimeState{Exists: false}, nil
	}

	services, err := inspectContainers(ctx, ids)
	if err != nil {
		return RuntimeState{}, err
	}
	return RuntimeState{Exists: true, Services: services}, nil
}

// ListRunning returns every RUNNING container on the host (not just managed) with
// its labels and bind-mount sources — B2's reality-based liveness query.
func (c *Compose) ListRunning(ctx context.Context) ([]ServiceState, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-q").Output() // running only
	if err != nil {
		return nil, fmt.Errorf("docker ps (running): %w", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) == 0 {
		return nil, nil
	}
	return inspectContainers(ctx, ids)
}

// dockerPS returns the IDs of llm-serve-managed containers for a compose project.
func (c *Compose) dockerPS(ctx context.Context, projectName string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "label=com.docker.compose.project="+projectName,
		"--filter", "label="+managedByLabel)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return out, nil
}

// inspectContainers runs `docker inspect` over container IDs and parses each into
// a ServiceState (name, running, restart count, labels). Shared by Inspect and
// ListManaged.
func inspectContainers(ctx context.Context, ids []string) ([]ServiceState, error) {
	args := append([]string{"inspect", "--format", "{{json .}}"}, ids...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}
	var services []ServiceState
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var ins struct {
			Name         string `json:"Name"`
			RestartCount int    `json:"RestartCount"`
			State        struct {
				Running bool `json:"Running"`
			} `json:"State"`
			Config struct {
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
			Mounts []struct {
				Type   string `json:"Type"`
				Source string `json:"Source"`
			} `json:"Mounts"`
		}
		if err := json.Unmarshal([]byte(line), &ins); err != nil {
			return nil, fmt.Errorf("parsing docker inspect: %w", err)
		}
		var mounts []string
		for _, m := range ins.Mounts {
			if m.Type == "bind" && m.Source != "" {
				mounts = append(mounts, m.Source)
			}
		}
		services = append(services, ServiceState{
			Name:         strings.TrimPrefix(ins.Name, "/"),
			Running:      ins.State.Running,
			RestartCount: ins.RestartCount,
			Labels:       ins.Config.Labels,
			Mounts:       mounts,
		})
	}
	return services, nil
}
