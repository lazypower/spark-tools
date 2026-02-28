package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirs_XDGDefaults(t *testing.T) {
	// Clear overrides
	for _, k := range []string{"LLM_BENCH_HOME", "LLM_BENCH_CONFIG_DIR", "LLM_BENCH_DATA_DIR", "LLM_BENCH_CACHE_DIR"} {
		t.Setenv(k, "")
	}
	// Clear XDG overrides too
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, "")
	}

	d := Dirs()
	home, _ := os.UserHomeDir()

	if !strings.HasSuffix(d.Config, filepath.Join(".config", "llm-bench")) {
		t.Errorf("config: got %q, want suffix %q", d.Config, filepath.Join(".config", "llm-bench"))
	}
	if d.Config != filepath.Join(home, ".config", "llm-bench") {
		t.Errorf("config: got %q", d.Config)
	}
	if !strings.HasSuffix(d.Data, filepath.Join(".local", "share", "llm-bench")) {
		t.Errorf("data: got %q", d.Data)
	}
	if !strings.HasSuffix(d.Cache, filepath.Join(".cache", "llm-bench")) {
		t.Errorf("cache: got %q", d.Cache)
	}
}

func TestDirs_HomeOverride(t *testing.T) {
	t.Setenv("LLM_BENCH_HOME", "/tmp/bench-home")
	t.Setenv("LLM_BENCH_CONFIG_DIR", "")
	t.Setenv("LLM_BENCH_DATA_DIR", "")
	t.Setenv("LLM_BENCH_CACHE_DIR", "")

	d := Dirs()
	if d.Config != "/tmp/bench-home/config" {
		t.Errorf("config: got %q", d.Config)
	}
	if d.Data != "/tmp/bench-home/data" {
		t.Errorf("data: got %q", d.Data)
	}
	if d.Cache != "/tmp/bench-home/cache" {
		t.Errorf("cache: got %q", d.Cache)
	}
}

func TestDirs_IndividualOverrides(t *testing.T) {
	t.Setenv("LLM_BENCH_HOME", "/tmp/bench-home")
	t.Setenv("LLM_BENCH_CONFIG_DIR", "/custom/config")
	t.Setenv("LLM_BENCH_DATA_DIR", "/custom/data")
	t.Setenv("LLM_BENCH_CACHE_DIR", "/custom/cache")

	d := Dirs()
	if d.Config != "/custom/config" {
		t.Errorf("config: got %q, want /custom/config", d.Config)
	}
	if d.Data != "/custom/data" {
		t.Errorf("data: got %q, want /custom/data", d.Data)
	}
	if d.Cache != "/custom/cache" {
		t.Errorf("cache: got %q, want /custom/cache", d.Cache)
	}
}

func TestDirs_XDGOverrides(t *testing.T) {
	t.Setenv("LLM_BENCH_HOME", "")
	t.Setenv("LLM_BENCH_CONFIG_DIR", "")
	t.Setenv("LLM_BENCH_DATA_DIR", "")
	t.Setenv("LLM_BENCH_CACHE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")

	d := Dirs()
	if d.Config != "/xdg/config/llm-bench" {
		t.Errorf("config: got %q", d.Config)
	}
	if d.Data != "/xdg/data/llm-bench" {
		t.Errorf("data: got %q", d.Data)
	}
	if d.Cache != "/xdg/cache/llm-bench" {
		t.Errorf("cache: got %q", d.Cache)
	}
}
