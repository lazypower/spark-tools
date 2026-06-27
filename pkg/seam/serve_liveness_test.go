package seam

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmserve"
	"github.com/lazypower/spark-tools/pkg/llmserve/instance"
	"github.com/lazypower/spark-tools/pkg/llmserve/lifecycle"
	"github.com/lazypower/spark-tools/pkg/llmserve/liveness"
	"github.com/lazypower/spark-tools/pkg/llmserve/runtime"
	"github.com/lazypower/spark-tools/pkg/llmserve/serving"
)

// Seam: B1 emit (the identity labels it stamps) <-> B2 liveness (the labels it
// reads to map a running container to the artifact it protects).
//
// CONTRACT: a container emitted for an instance must carry the artifact-host-path
// label B2 reads, and that label's value must be the model dir B2 protects. If
// emit stopped stamping it (or stamped a different key), B2 would protect the
// wrong path and B3 could evict a live model. The lifecycle.IdentityLabels
// function is the single definition both sides use; this guards that it actually
// carries the host artifact path.

type seamRuntime struct{ managed []runtime.ServiceState }

func (s *seamRuntime) Up(context.Context, string, string) error   { return nil }
func (s *seamRuntime) Down(context.Context, string, string) error { return nil }
func (s *seamRuntime) Inspect(context.Context, string, string) (runtime.RuntimeState, error) {
	return runtime.RuntimeState{}, nil
}
func (s *seamRuntime) ListManaged(context.Context) ([]runtime.ServiceState, error) {
	return s.managed, nil
}

func TestSeam_EmitLabels_ProtectArtifact(t *testing.T) {
	// Real (resolvable) artifact dirs — canonicalization is filesystem-based.
	models := t.TempDir()
	coder := filepath.Join(models, "Coder")
	other := filepath.Join(models, "SomethingElse")
	for _, d := range []string{coder, other} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	plan, _, err := llmserve.BuildPlan(llmserve.PlanRequest{
		Name:        "coder-next",
		Facts:       serving.ArtifactFacts{ModelID: "Qwen/Coder", ModelPath: coder, Arch: "Qwen3MoeForCausalLM", Tokenizer: serving.TokenizerQwen, Quant: serving.QuantNVFP4},
		Image:       "vllm/vllm-openai@v0.23.0",
		Accelerator: "nvidia:gb10:sm121",
		Mounts:      []llmserve.Mount{{Host: models, Container: "/models/hf"}},
		WatchdogDir: "/wd",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The labels emit stamps carry the host artifact path.
	labels := lifecycle.IdentityLabels(plan.Desired)
	if labels[lifecycle.LabelArtifactHostPath] != coder {
		t.Fatalf("SEAM: emit must stamp the host artifact path, got %q", labels[lifecycle.LabelArtifactHostPath])
	}

	// A running container with those exact labels must make liveness protect the
	// artifact dir — even with NO manifest (orphan case), proving the live half
	// reads what emit stamped.
	container := runtime.ServiceState{Name: "coder-next", Running: true, Labels: labels}
	lv := liveness.New(instance.NewStore(t.TempDir()), &seamRuntime{managed: []runtime.ServiceState{container}})

	if !lv.IsProtected(context.Background(), coder) {
		t.Error("SEAM CONTRACT BROKEN: a running emitted container must protect its host artifact dir")
	}
	if lv.IsProtected(context.Background(), other) {
		t.Error("an unrelated artifact must stay evictable")
	}
}
