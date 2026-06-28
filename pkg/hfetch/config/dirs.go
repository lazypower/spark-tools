package config

import (
	"os"
	"path/filepath"

	"github.com/lazypower/spark-tools/internal/paths"
)

const appName = "hfetch"

// DirConfig holds the resolved XDG directory paths.
type DirConfig struct {
	Config string // config dir (token, settings)
	Data   string // data dir (models, manifest)
	Cache  string // cache dir (API metadata)
}

// Dirs returns the resolved directory paths following the override rules:
//
//  1. Individual HFETCH_CONFIG_DIR / HFETCH_DATA_DIR / HFETCH_CACHE_DIR (highest priority)
//  2. HFETCH_HOME convenience shortcut (remaps all three)
//  3. XDG defaults (lowest priority)
func Dirs() DirConfig {
	d := DirConfig{
		Config: paths.XDGConfig(appName),
		Data:   paths.XDGData(appName),
		Cache:  paths.XDGCache(appName),
	}

	// HFETCH_HOME remaps all three as a convenience.
	if home := os.Getenv("HFETCH_HOME"); home != "" {
		d.Config = filepath.Join(home, "config")
		d.Data = filepath.Join(home, "data")
		d.Cache = filepath.Join(home, "cache")
	}

	// Individual overrides take highest priority.
	if v := os.Getenv("HFETCH_CONFIG_DIR"); v != "" {
		d.Config = v
	}
	if v := os.Getenv("HFETCH_DATA_DIR"); v != "" {
		d.Data = v
	}
	if v := os.Getenv("HFETCH_CACHE_DIR"); v != "" {
		d.Cache = v
	}

	return d
}

// XDG base resolution now delegates to internal/paths (the shared mechanism).
// hfetch retains its own POLICY above: the HFETCH_HOME remap and the individual
// HFETCH_*_DIR overrides.
