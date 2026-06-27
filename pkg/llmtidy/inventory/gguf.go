package inventory

import (
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// GGUFList walks the hfetch registry and returns one InstalledModel per
// completed GGUF file. A single repo with multiple quants therefore
// contributes multiple rows.
//
// Only .gguf files are surfaced. The hfetch registry is shared state: an
// `hfetch pull --profile vllm` records non-GGUF serve-ready files (safetensors
// shards, config, tokenizer) too, and those must not be miscategorized as GGUF
// models here (enforced by pkg/seam).
func GGUFList(r *registry.Registry) ([]InstalledModel, error) {
	if err := r.Load(); err != nil {
		return nil, err
	}
	var out []InstalledModel
	for _, lm := range r.List() {
		for _, f := range lm.Files {
			if !f.Complete || !strings.HasSuffix(f.Filename, ".gguf") {
				continue
			}
			name := lm.ID
			if f.Quantization != "" {
				name = lm.ID + " " + f.Quantization
			}
			out = append(out, InstalledModel{
				Name:     name,
				Backend:  BackendGGUF,
				Size:     f.Size,
				Modified: f.DownloadedAt,
				Repo:     lm.ID,
				Quant:    f.Quantization,
				Filename: f.Filename,
			})
		}
	}
	return out, nil
}

// GGUFDelete removes a single file from the hfetch registry and disk.
func GGUFDelete(r *registry.Registry, m InstalledModel) error {
	if m.Filename == "" {
		return r.Remove(m.Repo)
	}
	if err := r.Remove(m.Repo, m.Filename); err != nil {
		return err
	}
	return r.Save()
}
