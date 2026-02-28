package registry

import (
	"os"
	"path/filepath"
	"strings"
)

// GC removes partial downloads and orphaned files.
// Returns the total bytes freed.
func (r *Registry) GC() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var freed int64

	modelsDir := filepath.Join(r.dataDir, "models")
	entries, err := os.ReadDir(modelsDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Build set of known complete file paths.
	known := make(map[string]bool)
	for _, m := range r.manifest.Models {
		for _, f := range m.Files {
			if f.Complete {
				known[f.LocalPath] = true
			}
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(modelsDir, entry.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}

		for _, f := range files {
			filePath := filepath.Join(dirPath, f.Name())

			// Remove .partial and .state sidecar files.
			if strings.HasSuffix(f.Name(), ".partial") || strings.HasSuffix(f.Name(), ".state") {
				info, err := f.Info()
				if err == nil {
					freed += info.Size()
				}
				os.Remove(filePath)
				continue
			}

			// Remove orphaned files not tracked in the manifest.
			if !known[filePath] {
				info, err := f.Info()
				if err == nil {
					freed += info.Size()
				}
				os.Remove(filePath)
			}
		}

		// Remove empty directories.
		remaining, _ := os.ReadDir(dirPath)
		if len(remaining) == 0 {
			os.Remove(dirPath)
		}
	}

	return freed, nil
}
