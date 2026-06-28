package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

// removeDir deletes a model directory, tolerating an already-absent path.
func removeDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// VLLMList walks the hfetch registry and returns one InstalledModel per
// HF-format (safetensors) model — at MODEL-DIRECTORY granularity, unlike GGUF's
// per-file rows. A model is vLLM-format when it has at least one complete
// `.safetensors` file (the boundary that keeps GGUF and vLLM rows distinct;
// enforced by pkg/seam). Path is the model's actual on-disk directory (derived
// from a file's LocalPath, NOT the registry default — vLLM models are commonly
// pulled to a custom `--output` location), which is exactly what llm-serve
// protects, so the eviction interlock keys on the right path.
func VLLMList(r *registry.Registry) ([]InstalledModel, error) {
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

// VLLMDelete removes a vLLM model: its actual on-disk directory plus the registry
// entry. It deletes the real Path (covering a custom --output location), then
// clears the registry record (which also removes the default model dir if present).
func VLLMDelete(r *registry.Registry, m InstalledModel) error {
	if m.Path != "" {
		if err := removeDir(m.Path); err != nil {
			return err
		}
	}
	if err := r.Remove(m.Repo); err != nil {
		return err
	}
	return r.Save()
}
