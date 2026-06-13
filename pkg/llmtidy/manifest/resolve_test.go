package manifest

import (
	"path/filepath"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvManifest, EnvConfigDir, EnvXDGConfig, "HOME"} {
		t.Setenv(k, "")
	}
}

func TestResolveFlagWins(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvManifest, "/env/m.yaml")
	t.Setenv(EnvConfigDir, "/env/dir")
	t.Setenv(EnvXDGConfig, "/xdg")

	got, err := Resolve("/flag/m.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/flag/m.yaml" {
		t.Errorf("flag should win: got %q", got)
	}
}

func TestResolveManifestEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvManifest, "/env/m.yaml")
	t.Setenv(EnvConfigDir, "/env/dir")

	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/env/m.yaml" {
		t.Errorf("LLM_TIDY_MANIFEST should win over config dir: got %q", got)
	}
}

func TestResolveConfigDirEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvConfigDir, "/custom/config")

	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/custom/config", DefaultFilename)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveXDGFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvXDGConfig, "/xdg")

	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/xdg", AppName, DefaultFilename)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigDirHomeFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("HOME", "/home/test")

	got, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/home/test", ".config", AppName)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
