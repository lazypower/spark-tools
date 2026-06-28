package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/internal/modelstore"
)

// VLLMList walks the hfetch registry and returns one InstalledModel per
// HF-format (safetensors) model — at MODEL-DIRECTORY granularity, unlike GGUF's
// per-file rows. A model is vLLM-format when it has at least one complete
// `.safetensors` file (the boundary that keeps GGUF and vLLM rows distinct;
// enforced by pkg/seam). Path is the model's actual on-disk directory (derived
// from a file's LocalPath, NOT the registry default — vLLM models are commonly
// pulled to a custom `--output` location), which is exactly what llm-serve
// protects, so the eviction interlock keys on the right path.
func VLLMList(r *modelstore.Registry) ([]InstalledModel, error) {
	if err := r.Load(); err != nil {
		return nil, err
	}
	var out []InstalledModel
	for _, lm := range r.List() {
		var (
			dir      string
			size     int64
			modified time.Time
			isVLLM   bool
		)
		for _, f := range lm.Files {
			if !f.Complete {
				continue
			}
			if strings.HasSuffix(f.Filename, ".safetensors") {
				isVLLM = true
			}
			size += f.Size
			if dir == "" && f.LocalPath != "" {
				dir = filepath.Dir(f.LocalPath)
			}
			if f.DownloadedAt.After(modified) {
				modified = f.DownloadedAt
			}
		}
		if !isVLLM {
			continue
		}
		out = append(out, InstalledModel{
			Name:     lm.ID,
			Backend:  BackendVLLM,
			Size:     size,
			Modified: modified,
			Repo:     lm.ID,
			Path:     dir,
		})
	}
	return out, nil
}

// VLLMDelete removes a vLLM model by deleting ONLY this model's tracked files
// (by their LocalPath) — never RemoveAll-ing a directory, which would over-delete
// when a dir is shared (two models pulled to one --output, or a mixed
// gguf+safetensors repo). Any `.gguf` file is left intact so a mixed repo's GGUF
// model survives. An exclusively-empty model dir is then best-effort removed.
func VLLMDelete(r *modelstore.Registry, m InstalledModel) error {
	if err := r.Load(); err != nil {
		return err
	}
	lm := r.Get(m.Repo)
	if lm == nil {
		return nil // already gone
	}
	var names []string
	for _, f := range lm.Files {
		if !strings.HasSuffix(f.Filename, ".gguf") {
			names = append(names, f.Filename)
		}
	}
	if len(names) == 0 {
		return nil
	}
	if err := r.Remove(m.Repo, names...); err != nil {
		return err
	}
	if m.Path != "" {
		_ = os.Remove(m.Path) // rmdir only if now-empty (exclusive); errors harmlessly if shared
	}
	return r.Save()
}
