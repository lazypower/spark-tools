package config

import (
	"path/filepath"
	"testing"
)

func TestDirs_XDGDefaults(t *testing.T) {
	// Clear all overrides.
	for _, k := range []string{"HFETCH_HOME", "HFETCH_CONFIG_DIR", "HFETCH_DATA_DIR", "HFETCH_CACHE_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, "")
	}

	d := Dirs()

	if !filepath.IsAbs(d.Config) {
		t.Errorf("Config dir should be absolute, got %q", d.Config)
	}
	if filepath.Base(d.Config) != appName {
		t.Errorf("Config dir should end with %q, got %q", appName, d.Config)
	}
	if filepath.Base(d.Data) != appName {
		t.Errorf("Data dir should end with %q, got %q", appName, d.Data)
	}
	if filepath.Base(d.Cache) != appName {
		t.Errorf("Cache dir should end with %q, got %q", appName, d.Cache)
	}
}

func TestDirs_HFetchHome(t *testing.T) {
	t.Setenv("HFETCH_HOME", "/tmp/hftest")
	t.Setenv("HFETCH_CONFIG_DIR", "")
	t.Setenv("HFETCH_DATA_DIR", "")
	t.Setenv("HFETCH_CACHE_DIR", "")

	d := Dirs()

	if d.Config != "/tmp/hftest/config" {
		t.Errorf("expected /tmp/hftest/config, got %q", d.Config)
	}
	if d.Data != "/tmp/hftest/data" {
		t.Errorf("expected /tmp/hftest/data, got %q", d.Data)
	}
	if d.Cache != "/tmp/hftest/cache" {
		t.Errorf("expected /tmp/hftest/cache, got %q", d.Cache)
	}
}

func TestDirs_IndividualOverridesWin(t *testing.T) {
	t.Setenv("HFETCH_HOME", "/tmp/hftest")
	t.Setenv("HFETCH_CONFIG_DIR", "/override/config")
	t.Setenv("HFETCH_DATA_DIR", "")
	t.Setenv("HFETCH_CACHE_DIR", "")

	d := Dirs()

	if d.Config != "/override/config" {
		t.Errorf("individual override should win, got %q", d.Config)
	}
	// Data should still come from HFETCH_HOME.
	if d.Data != "/tmp/hftest/data" {
		t.Errorf("expected /tmp/hftest/data, got %q", d.Data)
	}
}

func TestDirs_XDGOverrides(t *testing.T) {
	t.Setenv("HFETCH_HOME", "")
	t.Setenv("HFETCH_CONFIG_DIR", "")
	t.Setenv("HFETCH_DATA_DIR", "")
	t.Setenv("HFETCH_CACHE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")

	d := Dirs()

	if d.Config != filepath.Join("/xdg/config", appName) {
		t.Errorf("expected /xdg/config/hfetch, got %q", d.Config)
	}
	if d.Data != filepath.Join("/xdg/data", appName) {
		t.Errorf("expected /xdg/data/hfetch, got %q", d.Data)
	}
	if d.Cache != filepath.Join("/xdg/cache", appName) {
		t.Errorf("expected /xdg/cache/hfetch, got %q", d.Cache)
	}
}
