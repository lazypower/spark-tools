package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirs_XDGDefaults(t *testing.T) {
	// Clear all overrides.
	for _, env := range []string{"LLM_RUN_HOME", "LLM_RUN_CONFIG_DIR", "LLM_RUN_DATA_DIR", "LLM_RUN_CACHE_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(env, "")
	}

	d := Dirs()
	home, _ := os.UserHomeDir()

	if d.Config != filepath.Join(home, ".config", "llm-run") {
		t.Errorf("config: got %s", d.Config)
	}
	if d.Data != filepath.Join(home, ".local", "share", "llm-run") {
		t.Errorf("data: got %s", d.Data)
	}
	if d.Cache != filepath.Join(home, ".cache", "llm-run") {
		t.Errorf("cache: got %s", d.Cache)
	}
}

func TestDirs_LLMRunHome(t *testing.T) {
	for _, env := range []string{"LLM_RUN_CONFIG_DIR", "LLM_RUN_DATA_DIR", "LLM_RUN_CACHE_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(env, "")
	}
	t.Setenv("LLM_RUN_HOME", "/opt/llm-run")

	d := Dirs()
	if d.Config != "/opt/llm-run/config" {
		t.Errorf("config: got %s", d.Config)
	}
	if d.Data != "/opt/llm-run/data" {
		t.Errorf("data: got %s", d.Data)
	}
	if d.Cache != "/opt/llm-run/cache" {
		t.Errorf("cache: got %s", d.Cache)
	}
}

func TestDirs_IndividualOverrides(t *testing.T) {
	t.Setenv("LLM_RUN_HOME", "/opt/llm-run")
	t.Setenv("LLM_RUN_CONFIG_DIR", "/custom/config")
	t.Setenv("LLM_RUN_DATA_DIR", "")
	t.Setenv("LLM_RUN_CACHE_DIR", "")

	d := Dirs()
	if d.Config != "/custom/config" {
		t.Errorf("config: got %s", d.Config)
	}
	// Data and cache should still come from LLM_RUN_HOME.
	if d.Data != "/opt/llm-run/data" {
		t.Errorf("data: got %s", d.Data)
	}
}

func TestGlobalConfig_Defaults(t *testing.T) {
	cfg := DefaultGlobalConfig()
	if cfg.DefaultProfile != "default" {
		t.Errorf("expected default profile 'default', got %q", cfg.DefaultProfile)
	}
}

func TestGlobalConfig_EnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LLM_RUN_HOME", tmp)
	t.Setenv("LLM_RUN_CONFIG_DIR", "")
	t.Setenv("LLM_RUN_DATA_DIR", "")
	t.Setenv("LLM_RUN_CACHE_DIR", "")
	t.Setenv("LLM_RUN_DEFAULT_MODEL", "test-model")
	t.Setenv("LLM_RUN_DEFAULT_PROFILE", "coding")
	t.Setenv("LLM_RUN_LLAMA_DIR", "/opt/llama")

	cfg := LoadGlobalConfig()
	if cfg.DefaultModel != "test-model" {
		t.Errorf("expected test-model, got %q", cfg.DefaultModel)
	}
	if cfg.DefaultProfile != "coding" {
		t.Errorf("expected coding, got %q", cfg.DefaultProfile)
	}
	if cfg.LlamaDir != "/opt/llama" {
		t.Errorf("expected /opt/llama, got %q", cfg.LlamaDir)
	}
}

func TestGlobalConfig_SaveLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LLM_RUN_HOME", tmp)
	t.Setenv("LLM_RUN_CONFIG_DIR", "")
	t.Setenv("LLM_RUN_DATA_DIR", "")
	t.Setenv("LLM_RUN_CACHE_DIR", "")
	t.Setenv("LLM_RUN_DEFAULT_MODEL", "")
	t.Setenv("LLM_RUN_DEFAULT_PROFILE", "")
	t.Setenv("LLM_RUN_LLAMA_DIR", "")

	cfg := GlobalConfig{
		DefaultModel:   "my-model",
		DefaultProfile: "precise",
		LlamaDir:       "/usr/local/bin",
	}
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded := LoadGlobalConfig()
	if loaded.DefaultModel != "my-model" {
		t.Errorf("expected my-model, got %q", loaded.DefaultModel)
	}
	if loaded.DefaultProfile != "precise" {
		t.Errorf("expected precise, got %q", loaded.DefaultProfile)
	}
}
