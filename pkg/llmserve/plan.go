package llmserve

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/servecontract"
	"github.com/lazypower/spark-tools/internal/serveinstance"
	"github.com/lazypower/spark-tools/internal/servespec"
	"github.com/lazypower/spark-tools/internal/serving"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
)

// PlanRequest is everything needed to turn a verified artifact + capabilities
// into a lifecycle bring-up plan: the contract inputs plus the host facts (image,
// port, mounts, watchdog) the emit driver specializes for.
type PlanRequest struct {
	Name         string // instance identity (and served-name default)
	ServedName   string // optional; defaults to Name
	Facts        serving.ArtifactFacts
	Capabilities []serving.Capability
	ContextLen   int
	GPUMemUtil   float64 // vLLM --gpu-memory-utilization fraction (0,1]; 0 = unset (hardware default, then vLLM's own)
	MaxNumSeqs   int     // vLLM --max-num-seqs (max concurrent sequences); 0 = unset (hardware default, then vLLM's own)
	Image        string  // engine image digest/tag (also the target engine fingerprint)
	Accelerator  string  // target accelerator fingerprint
	// ContainerEngine and Command are the same host facts `emit` takes. Without
	// them `up` renders the Docker GPU-access form and assumes the official
	// vllm entrypoint, so the primary lifecycle command reproduced both of the
	// failures the emit path already fixes.
	ContainerEngine string
	Command         []string
	Port            int // host port (default 8000)
	Mounts          []servespec.Mount
	WatchdogDir     string // host dir holding watchdog.sh (required for a serving instance)
}

// BuildPlan resolves the request into a validated contract and renders the
// compose spec (with identity labels + the watchdog sidecar), returning a
// lifecycle.Plan and the resolved contract (for surfacing warnings). The same
// IdentityLabels definition is used to stamp the spec here and to verify it in
// reconcile, so they cannot drift.
func BuildPlan(req PlanRequest) (lifecycle.Plan, *servecontract.Resolved, error) {
	if !serveinstance.ValidName(req.Name) {
		return lifecycle.Plan{}, nil, fmt.Errorf("invalid instance name %q", req.Name)
	}
	served := req.ServedName
	if served == "" {
		served = req.Name
	}
	port := req.Port
	if port == 0 {
		port = 8000
	}

	// Resolve all host paths to ABSOLUTE at servespec. The emitted spec is persisted
	// under XDG state and run by `docker compose` from THAT directory — a
	// caller-cwd-relative mount (e.g. ./models) would resolve against the spec's
	// dir, not the operator's cwd, and silently mount the wrong (empty) path. A
	// persisted, relocated spec must be self-contained.
	facts := req.Facts
	if facts.ModelPath != "" {
		abs, err := filepath.Abs(facts.ModelPath)
		if err != nil {
			return lifecycle.Plan{}, nil, fmt.Errorf("resolving model dir to absolute: %w", err)
		}
		facts.ModelPath = abs
	}
	mounts, err := absoluteMounts(req.Mounts)
	if err != nil {
		return lifecycle.Plan{}, nil, err
	}
	watchdogDir := req.WatchdogDir
	if watchdogDir != "" {
		if watchdogDir, err = filepath.Abs(watchdogDir); err != nil {
			return lifecycle.Plan{}, nil, fmt.Errorf("resolving watchdog dir to absolute: %w", err)
		}
	}

	creq := servecontract.Request{
		ServedName:   served,
		Capabilities: req.Capabilities,
		ContextLen:   req.ContextLen,
		GPUMemUtil:   req.GPUMemUtil,
		MaxNumSeqs:   req.MaxNumSeqs,
		Target:       fingerprint.Fingerprint{Engine: req.Image, Accelerator: req.Accelerator},
	}
	resolved, err := servecontract.Resolve(creq, facts)
	if err != nil {
		return lifecycle.Plan{}, nil, err
	}

	project := "llm-serve-" + req.Name
	desired := serveinstance.Desired{
		Name:          req.Name,
		ServedName:    served,
		ModelID:       facts.ModelID,
		ModelRevision: facts.Revision,
		ModelDir:      facts.ModelPath,
		ContractKey:   resolved.Key,
		Target:        fingerprint.Fingerprint{Engine: req.Image, Accelerator: req.Accelerator},
		ProjectName:   project,
		Endpoint:      fmt.Sprintf("http://localhost:%d", port),
	}

	// Host without labels first, so the spec hash (a label) is computed over the
	// command/image/mounts, not over itself.
	host := servespec.Host{
		Image:           imageRef(req.Image),
		Port:            port,
		Volumes:         mounts,
		Accelerator:     req.Accelerator,
		ContainerEngine: req.ContainerEngine,
		Command:         req.Command,
	}
	if watchdogDir != "" {
		host.Watchdog = &servespec.Watchdog{ScriptHostDir: watchdogDir, Project: project}
	}
	desired.SpecHash = servespec.SpecHash(resolved, host)

	// Now stamp the identity labels (which include the spec hash) and render.
	host.Labels = lifecycle.IdentityLabels(desired)
	spec, err := servespec.Render(servespec.TargetCompose, resolved, host)
	if err != nil {
		return lifecycle.Plan{}, nil, err
	}

	return lifecycle.Plan{Desired: desired, Spec: spec}, resolved, nil
}

// absoluteMounts resolves each mount's HOST path to absolute (its container path
// is already absolute). This is what makes the persisted spec self-contained: a
// relative host path would otherwise resolve against the spec's storage dir.
func absoluteMounts(in []servespec.Mount) ([]servespec.Mount, error) {
	out := make([]servespec.Mount, len(in))
	for i, m := range in {
		abs, err := filepath.Abs(m.Host)
		if err != nil {
			return nil, fmt.Errorf("resolving mount host %q to absolute: %w", m.Host, err)
		}
		out[i] = servespec.Mount{Host: abs, Container: m.Container}
	}
	return out, nil
}

// imageRef converts a fingerprint-style engine ref (image@tag) into a runnable
// image reference for the container runtime; a real digest (image@sha256:…) is
// left as-is, a tag suffix (image@v0.23.0) becomes image:v0.23.0.
func imageRef(image string) string {
	i := strings.LastIndexByte(image, '@')
	if i < 0 {
		return image
	}
	suffix := image[i+1:]
	if strings.ContainsRune(suffix, ':') {
		return image
	}
	return image[:i] + ":" + suffix
}
