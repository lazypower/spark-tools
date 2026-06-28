// Package runconfig handles XDG directory resolution and global
// configuration for llm-run.
package runconfig

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lazypower/spark-tools/internal/paths"
)

const appName = "llm-run"

// DirConfig holds the resolved XDG directory paths.
type DirConfig struct {
	Config string // config dir (profiles, aliases, settings)
	Data   string // data dir (PID files, logs)
	Cache  string // cache dir (capability probes)
}

// Dirs returns the resolved directory paths following the override rules:
//
//  1. Individual LLM_RUN_CONFIG_DIR / LLM_RUN_DATA_DIR / LLM_RUN_CACHE_DIR (highest priority)
//  2. LLM_RUN_HOME convenience shortcut (remaps all three)
//  3. XDG defaults (lowest priority)
func Dirs() DirConfig {
	d := DirConfig{
		Config: paths.XDGConfig(appName),
		Data:   paths.XDGData(appName),
		Cache:  paths.XDGCache(appName),
	}

	if home := os.Getenv("LLM_RUN_HOME"); home != "" {
		d.Config = filepath.Join(home, "config")
		d.Data = filepath.Join(home, "data")
		d.Cache = filepath.Join(home, "cache")
	}

	if v := os.Getenv("LLM_RUN_CONFIG_DIR"); v != "" {
		d.Config = v
	}
	if v := os.Getenv("LLM_RUN_DATA_DIR"); v != "" {
		d.Data = v
	}
	if v := os.Getenv("LLM_RUN_CACHE_DIR"); v != "" {
		d.Cache = v
	}

	return d
}

// GlobalConfig holds user preferences from config.json.
type GlobalConfig struct {
	DefaultModel   string `json:"default_model,omitempty"`
	DefaultProfile string `json:"default_profile,omitempty"`
	LlamaDir       string `json:"llama_dir,omitempty"`
}

// DefaultGlobalConfig returns sensible defaults.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		DefaultProfile: "default",
	}
}

// LoadGlobalConfig reads config.json from the config directory.
// Falls back to env vars, then defaults.
func LoadGlobalConfig() GlobalConfig {
	cfg := DefaultGlobalConfig()

	dirs := Dirs()
	data, err := os.ReadFile(filepath.Join(dirs.Config, "config.json"))
	if err == nil {
		json.Unmarshal(data, &cfg)
	}

	// Env overrides take priority over config file.
	if v := os.Getenv("LLM_RUN_DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := os.Getenv("LLM_RUN_DEFAULT_PROFILE"); v != "" {
		cfg.DefaultProfile = v
	}
	if v := os.Getenv("LLM_RUN_LLAMA_DIR"); v != "" {
		cfg.LlamaDir = v
	}

	return cfg
}

// SaveGlobalConfig writes config.json to the config directory.
func SaveGlobalConfig(cfg GlobalConfig) error {
	dirs := Dirs()
	if err := os.MkdirAll(dirs.Config, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dirs.Config, "config.json"), data, 0644)
}

// XDG base resolution now delegates to internal/paths (the shared mechanism).
// llm-run retains its own POLICY above: the LLM_RUN_HOME remap and the
// individual LLM_RUN_*_DIR overrides.
