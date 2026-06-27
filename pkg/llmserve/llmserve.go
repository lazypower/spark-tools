// Package llmserve is the unified Go API for the llm-serve contract engine: the
// vLLM sibling to llmrun. It composes the sub-packages (serving vocabulary,
// arch profiles, fingerprint, contract resolution, artifact detection, emit
// drivers) so a consumer imports one package to turn a request + a verified
// artifact into a host-appropriate launch spec.
//
// v1 is emit-only (design §6): it resolves and emits, and owns nothing at
// runtime. Launch, supervision, registration, and the llm-tidy interlock are v2
// (B) — foreclosed here by construction.
package llmserve

import (
	"os"
	"path/filepath"

	"github.com/lazypower/spark-tools/pkg/llmserve/artifact"
	"github.com/lazypower/spark-tools/pkg/llmserve/contract"
	"github.com/lazypower/spark-tools/pkg/llmserve/emit"
	"github.com/lazypower/spark-tools/pkg/llmserve/fingerprint"
	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/liveness"
	"github.com/lazypower/spark-tools/pkg/llmserve/profiles"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// Re-export the common types so consumers import only llmserve.
type (
	Capability    = serving.Capability
	QuantMethod   = serving.QuantMethod
	ArtifactFacts = serving.ArtifactFacts
	ContractKey   = serving.ContractKey
	Fingerprint   = fingerprint.Fingerprint
	Request       = contract.Request
	Resolved      = contract.Resolved
	ArchProfile   = profiles.ArchProfile
	Host          = emit.Host
	Mount         = emit.Mount
	Target        = emit.Target

	// B1 lifecycle re-exports.
	Orchestrator    = lifecycle.Orchestrator
	LifecyclePlan   = lifecycle.Plan
	LifecycleResult = lifecycle.Result
	ServingStatus   = lifecycle.ServingStatus
	InstanceStatus  = lifecycle.InstanceStatus
)

// Re-export the capability vocabulary.
const (
	GuidedDecoding = serving.GuidedDecoding
	Thinking       = serving.Thinking
	ToolCalling    = serving.ToolCalling
	Vision         = serving.Vision
)

// EmitResult is the outcome of an end-to-end emit: the rendered launch spec, the
// validated contract behind it, and the render target used.
type EmitResult struct {
	Spec     string
	Resolved *contract.Resolved
	Target   emit.Target
}

// Emit resolves a request against the supplied verified artifact facts and
// renders it for the given target and host. It is the one-call path: the caller
// is responsible for having obtained facts from a VERIFIED artifact (see
// DetectFacts / VerifyArtifact), since emit-only v1 trusts the artifact gate
// upstream.
func Emit(req contract.Request, facts serving.ArtifactFacts, target emit.Target, host emit.Host) (*EmitResult, error) {
	resolved, err := contract.Resolve(req, facts)
	if err != nil {
		return nil, err
	}
	spec, err := emit.Render(target, resolved, host)
	if err != nil {
		return nil, err
	}
	return &EmitResult{Spec: spec, Resolved: resolved, Target: target}, nil
}

// DetectFacts reads serving facts from a local model directory whose
// completeness has already been verified (e.g. by an hfetch pull).
func DetectFacts(dir string) (serving.ArtifactFacts, error) {
	return artifact.DetectFacts(dir)
}

// BuiltinProfiles returns the v1 arch-profile registry.
func BuiltinProfiles() []profiles.ArchProfile { return profiles.BuiltinProfiles() }

// Targets returns the supported render targets.
func Targets() []emit.Target { return emit.Targets() }

// NewOrchestrator wires the B1 lifecycle over the real compose runtime and HTTP
// prober, with manifests under stateDir and emitted specs under specDir.
func NewOrchestrator(stateDir, specDir string) *lifecycle.Orchestrator {
	return &lifecycle.Orchestrator{
		Store:   instance.NewStore(stateDir),
		Runtime: runtime.NewCompose(),
		Prober:  runtime.NewHTTPProber(),
		SpecDir: specDir,
	}
}

// NewLiveness wires the B2 liveness authority over the manifest store and the
// real compose runtime. It answers "is this artifact protected from eviction?"
// for tools like llm-tidy — derived live, fail-closed.
func NewLiveness(stateDir string) *liveness.Liveness {
	return liveness.New(instance.NewStore(stateDir), runtime.NewCompose())
}

// EnsureWatchdogScript writes the embedded watchdog.sh into dir (idempotently) so
// the emitted compose's /watchdog mount resolves. Returns the dir on success.
func EnsureWatchdogScript(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "watchdog.sh"), []byte(emit.WatchdogScript), 0755); err != nil {
		return "", err
	}
	return dir, nil
}
