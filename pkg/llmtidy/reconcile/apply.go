package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/lazypower/spark-tools/pkg/llmtidy/inventory"
)

// PruneEvent is emitted as each model is removed.
type PruneEvent struct {
	Model inventory.InstalledModel
	Err   error
}

// Prune removes every model in plan via the provider and returns the list of
// successfully removed models and total bytes reclaimed. Per-model failures
// are reported via onEvent (when non-nil) and aggregated into the error
// return; one failure does not abort the rest of the plan.
func Prune(
	ctx context.Context,
	p *inventory.Provider,
	plan []inventory.InstalledModel,
	onEvent func(PruneEvent),
) ([]inventory.InstalledModel, int64, error) {
	var (
		removed []inventory.InstalledModel
		bytes   int64
		errs    []error
	)
	for _, m := range plan {
		err := p.Delete(ctx, m)
		if onEvent != nil {
			onEvent(PruneEvent{Model: m, Err: err})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Name, err))
			continue
		}
		removed = append(removed, m)
		bytes += m.Size
	}
	return removed, bytes, errors.Join(errs...)
}

// Syncer is the dependency injected by the CLI to execute a sync plan. The
// interface lets tests substitute fakes and lets the v0.1 implementation
// own the choice of how to talk to each remote.
type Syncer interface {
	// PullOllama issues an Ollama-side pull. onStatus may be nil.
	PullOllama(ctx context.Context, name string, onStatus func(string)) error

	// PullGGUF resolves the file matching repo+quant on HuggingFace and
	// downloads it via hfetch. quant may be empty.
	PullGGUF(ctx context.Context, repo, quant string, onStatus func(string)) error
}

// SyncEvent is emitted as each spec is processed.
type SyncEvent struct {
	Spec   ModelSpec
	Status string // free-form, e.g. "pulling", "ok", "skipped"
	Err    error
}

// Sync pulls every missing spec via the syncer. Per-spec failures are
// reported via onEvent (when non-nil) and aggregated into the error return.
func Sync(
	ctx context.Context,
	s Syncer,
	plan []ModelSpec,
	onEvent func(SyncEvent),
) error {
	var errs []error
	for _, spec := range plan {
		emit := func(status string, err error) {
			if onEvent != nil {
				onEvent(SyncEvent{Spec: spec, Status: status, Err: err})
			}
		}
		var err error
		switch spec.Backend {
		case inventory.BackendOllama:
			if spec.Ollama == nil {
				err = errors.New("ollama spec missing payload")
				break
			}
			emit("pulling", nil)
			err = s.PullOllama(ctx, spec.Ollama.NormalizedName(), func(line string) {
				emit(line, nil)
			})
		case inventory.BackendGGUF:
			if spec.GGUF == nil {
				err = errors.New("gguf spec missing payload")
				break
			}
			emit("pulling", nil)
			err = s.PullGGUF(ctx, spec.GGUF.Repo, spec.GGUF.Quant, func(line string) {
				emit(line, nil)
			})
		case inventory.BackendVLLM:
			// vLLM pull (the full safetensors fileset) is not yet wired into sync;
			// skip rather than fail so a vllm: manifest entry doesn't break sync.
			// Pull manually with `hfetch pull --dest vllm` for now.
			emit("skipped (vLLM sync not yet supported; use hfetch pull --dest vllm)", nil)
			continue
		default:
			err = fmt.Errorf("unsupported backend %v", spec.Backend)
		}
		if err != nil {
			emit("error", err)
			errs = append(errs, fmt.Errorf("%s: %w", spec.Name(), err))
			continue
		}
		emit("ok", nil)
	}
	return errors.Join(errs...)
}
