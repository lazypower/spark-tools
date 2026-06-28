// Package config is a compatibility wrapper over internal/runconfig. The llm-run
// XDG directory resolution and global-config load/save moved to
// internal/runconfig during the /internal extraction (named to disambiguate from
// hfetch's config); this thin alias keeps existing importers (cmd/llm-run,
// pkg/llmrun) compiling unchanged until they migrate. Type aliases keep the config
// structs flowing across the boundary; the funcs delegate to the authority.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/runconfig.
package config

import (
	rc "github.com/lazypower/spark-tools/internal/runconfig"
)

// Type aliases — keep values flowing across the boundary as the same type.
type (
	DirConfig    = rc.DirConfig
	GlobalConfig = rc.GlobalConfig
)

// Dirs resolves the llm-run XDG directory set (honoring LLM_RUN_HOME and the
// per-dir overrides).
func Dirs() DirConfig { return rc.Dirs() }

// DefaultGlobalConfig returns the built-in default global config.
func DefaultGlobalConfig() GlobalConfig { return rc.DefaultGlobalConfig() }

// LoadGlobalConfig loads the global config, falling back to defaults.
func LoadGlobalConfig() GlobalConfig { return rc.LoadGlobalConfig() }

// SaveGlobalConfig persists the global config.
func SaveGlobalConfig(cfg GlobalConfig) error { return rc.SaveGlobalConfig(cfg) }
