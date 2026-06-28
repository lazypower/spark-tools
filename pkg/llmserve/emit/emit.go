// Package emit is a compatibility wrapper over internal/servespec. The launch-spec
// render-target driver (compose / docker-run / quadlet rendering over one shared
// Spec, plus the watchdog sidecar and the SpecHash identity hash) moved to
// internal/servespec during the /internal extraction; this thin alias keeps
// existing importers (pkg/llmserve, cmd/llm-serve) compiling unchanged until they
// migrate. Type aliases carry the Host/Watchdog methods over; the render funcs,
// the Target enum, and the embedded WatchdogScript delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/servespec.
package emit

import (
	sc "github.com/lazypower/spark-tools/internal/servecontract"
	ss "github.com/lazypower/spark-tools/internal/servespec"
)

// Type aliases — carry methods over and keep values flowing across the boundary
// as the same type.
type (
	Watchdog = ss.Watchdog
	Host     = ss.Host
	Mount    = ss.Mount
	Target   = ss.Target
)

// Render targets.
const (
	TargetCompose   = ss.TargetCompose
	TargetDockerRun = ss.TargetDockerRun
	TargetQuadlet   = ss.TargetQuadlet
)

// WatchdogScript is the engine-wedge watchdog script (embedded in the authority).
var WatchdogScript = ss.WatchdogScript

// Targets returns the supported render targets in stable order.
func Targets() []Target { return ss.Targets() }

// DockerRun renders the launch spec as a `docker run` command.
func DockerRun(r *sc.Resolved, h Host) string { return ss.DockerRun(r, h) }

// Compose renders the launch spec as a docker-compose file.
func Compose(r *sc.Resolved, h Host) string { return ss.Compose(r, h) }

// Quadlet renders the launch spec as a podman quadlet unit.
func Quadlet(r *sc.Resolved, h Host) string { return ss.Quadlet(r, h) }

// SpecHash is a stable content hash of the runtime-identity-relevant inputs.
func SpecHash(r *sc.Resolved, h Host) string { return ss.SpecHash(r, h) }

// Render renders the launch spec for the given target.
func Render(target Target, r *sc.Resolved, h Host) (string, error) { return ss.Render(target, r, h) }
