// Package inventory is a compatibility wrapper over internal/inventory. The
// installed-model enumeration across the Ollama/GGUF/vLLM backends moved to
// internal/inventory during the /internal extraction; this thin alias keeps
// existing importers (pkg/llmtidy, pkg/llmtidy/{interlock,reconcile},
// cmd/llm-tidy, pkg/seam) compiling unchanged until they migrate. Type aliases
// carry the Provider/ModelBackend methods over; the backend list/delete funcs
// and ParseBackend delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/inventory.
package inventory

import (
	"context"

	iinv "github.com/lazypower/spark-tools/internal/inventory"
	"github.com/lazypower/spark-tools/internal/modelstore"
	"github.com/lazypower/spark-tools/internal/ollama"
)

// Type aliases — carry methods (ModelBackend.String, Provider.Probe/All/
// AllByBackend/Delete) over and keep values flowing across the boundary as the
// same type.
type (
	ModelBackend   = iinv.ModelBackend
	InstalledModel = iinv.InstalledModel
	Provider       = iinv.Provider
	Available      = iinv.Available
)

// Backend enum.
const (
	BackendUnknown = iinv.BackendUnknown
	BackendOllama  = iinv.BackendOllama
	BackendGGUF    = iinv.BackendGGUF
	BackendVLLM    = iinv.BackendVLLM
)

// ParseBackend converts a CLI flag value to a ModelBackend.
func ParseBackend(s string) (ModelBackend, error) { return iinv.ParseBackend(s) }

// GGUFList walks the hfetch registry and returns one InstalledModel per .gguf.
func GGUFList(r *modelstore.Registry) ([]InstalledModel, error) { return iinv.GGUFList(r) }

// GGUFDelete removes a single file from the hfetch registry and disk.
func GGUFDelete(r *modelstore.Registry, m InstalledModel) error { return iinv.GGUFDelete(r, m) }

// VLLMList walks the hfetch registry and returns one InstalledModel per vLLM dir.
func VLLMList(r *modelstore.Registry) ([]InstalledModel, error) { return iinv.VLLMList(r) }

// VLLMDelete removes a vLLM model directory via the registry.
func VLLMDelete(r *modelstore.Registry, m InstalledModel) error { return iinv.VLLMDelete(r, m) }

// OllamaList queries the Ollama server and returns its installed models.
func OllamaList(ctx context.Context, c *ollama.Client) ([]InstalledModel, error) {
	return iinv.OllamaList(ctx, c)
}

// OllamaDelete removes a model via the Ollama REST API.
func OllamaDelete(ctx context.Context, c *ollama.Client, m InstalledModel) error {
	return iinv.OllamaDelete(ctx, c, m)
}
