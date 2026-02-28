// Package config handles XDG directory resolution for llm-bench.
package config

import (
	"os"
	"path/filepath"
)

const appName = "llm-bench"

// DirConfig holds the resolved XDG directory paths.
type DirConfig struct {
	Config string // config dir (settings)
	Data   string // data dir (results)
	Cache  string // cache dir (tokenization cache)
}

// Dirs returns the resolved directory paths following the override rules:
//
//  1. Individual LLM_BENCH_CONFIG_DIR / LLM_BENCH_DATA_DIR / LLM_BENCH_CACHE_DIR (highest priority)
//  2. LLM_BENCH_HOME convenience shortcut (remaps all three)
//  3. XDG defaults (lowest priority)
func Dirs() DirConfig {
	d := DirConfig{
		Config: xdgConfig(),
		Data:   xdgData(),
		Cache:  xdgCache(),
	}

	if home := os.Getenv("LLM_BENCH_HOME"); home != "" {
		d.Config = filepath.Join(home, "config")
		d.Data = filepath.Join(home, "data")
		d.Cache = filepath.Join(home, "cache")
	}

	if v := os.Getenv("LLM_BENCH_CONFIG_DIR"); v != "" {
		d.Config = v
	}
	if v := os.Getenv("LLM_BENCH_DATA_DIR"); v != "" {
		d.Data = v
	}
	if v := os.Getenv("LLM_BENCH_CACHE_DIR"); v != "" {
		d.Cache = v
	}

	return d
}

func xdgConfig() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", appName)
}

func xdgData() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", appName)
}

func xdgCache() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", appName)
}
