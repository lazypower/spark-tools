package inventory

import (
	"context"

	"github.com/lazypower/spark-tools/pkg/llmtidy/ollama"
)

// OllamaList queries the Ollama server and returns its installed models.
func OllamaList(ctx context.Context, c *ollama.Client) ([]InstalledModel, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InstalledModel, 0, len(models))
	for _, m := range models {
		out = append(out, InstalledModel{
			Name:       m.Name,
			Backend:    BackendOllama,
			Size:       m.Size,
			Modified:   m.ModifiedAt,
			OllamaName: m.Name,
		})
	}
	return out, nil
}

// OllamaDelete removes a model via the Ollama REST API.
func OllamaDelete(ctx context.Context, c *ollama.Client, m InstalledModel) error {
	return c.Delete(ctx, m.OllamaName)
}
