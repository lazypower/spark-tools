package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/lazypower/spark-tools/internal/modelstore"
	"github.com/lazypower/spark-tools/internal/ollama"
)

// Provider exposes installed models across every backend. GGUF and VLLM share
// one hfetch registry (they are the same store, distinguished by file type).
type Provider struct {
	Ollama *ollama.Client
	GGUF   *modelstore.Registry
	VLLM   *modelstore.Registry
}

// Available reports per-backend availability after a Probe.
type Available struct {
	Ollama bool
	GGUF   bool
	VLLM   bool
}

// Probe checks which backends are reachable. Ollama is checked with a
// short HTTP probe; GGUF and VLLM are "available" if the registry can be loaded.
func (p *Provider) Probe(ctx context.Context) Available {
	a := Available{}
	if p.Ollama != nil {
		a.Ollama = p.Ollama.Available(ctx)
	}
	if p.GGUF != nil {
		a.GGUF = p.GGUF.Load() == nil
	}
	if p.VLLM != nil {
		a.VLLM = p.VLLM.Load() == nil
	}
	return a
}

// All returns models across every backend the provider has configured. A
// backend that fails to list contributes its error via the returned
// multierror; partial inventories are returned alongside.
func (p *Provider) All(ctx context.Context) ([]InstalledModel, error) {
	var (
		out  []InstalledModel
		errs []error
	)
	if p.Ollama != nil {
		models, err := OllamaList(ctx, p.Ollama)
		if err != nil {
			errs = append(errs, fmt.Errorf("ollama: %w", err))
		} else {
			out = append(out, models...)
		}
	}
	if p.GGUF != nil {
		models, err := GGUFList(p.GGUF)
		if err != nil {
			errs = append(errs, fmt.Errorf("gguf: %w", err))
		} else {
			out = append(out, models...)
		}
	}
	if p.VLLM != nil {
		models, err := VLLMList(p.VLLM)
		if err != nil {
			errs = append(errs, fmt.Errorf("vllm: %w", err))
		} else {
			out = append(out, models...)
		}
	}
	return out, errors.Join(errs...)
}

// AllByBackend filters Provider.All to a single backend.
func (p *Provider) AllByBackend(ctx context.Context, b ModelBackend) ([]InstalledModel, error) {
	switch b {
	case BackendOllama:
		if p.Ollama == nil {
			return nil, errors.New("ollama backend not configured")
		}
		return OllamaList(ctx, p.Ollama)
	case BackendGGUF:
		if p.GGUF == nil {
			return nil, errors.New("gguf backend not configured")
		}
		return GGUFList(p.GGUF)
	case BackendVLLM:
		if p.VLLM == nil {
			return nil, errors.New("vllm backend not configured")
		}
		return VLLMList(p.VLLM)
	default:
		return nil, fmt.Errorf("unsupported backend %v", b)
	}
}

// Delete removes the model via its backend.
func (p *Provider) Delete(ctx context.Context, m InstalledModel) error {
	switch m.Backend {
	case BackendOllama:
		if p.Ollama == nil {
			return errors.New("ollama backend not configured")
		}
		return OllamaDelete(ctx, p.Ollama, m)
	case BackendGGUF:
		if p.GGUF == nil {
			return errors.New("gguf backend not configured")
		}
		return GGUFDelete(p.GGUF, m)
	case BackendVLLM:
		if p.VLLM == nil {
			return errors.New("vllm backend not configured")
		}
		return VLLMDelete(p.VLLM, m)
	default:
		return fmt.Errorf("unsupported backend %v", m.Backend)
	}
}
