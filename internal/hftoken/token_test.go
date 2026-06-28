package hftoken

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_FlagOverride(t *testing.T) {
	t.Setenv("HFETCH_TOKEN", "env_token")
	result := Resolve("flag_token", t.TempDir())
	if result.Token != "flag_token" || result.Source != "flag" {
		t.Errorf("expected flag override, got %+v", result)
	}
}

func TestResolve_EnvVar(t *testing.T) {
	t.Setenv("HFETCH_TOKEN", "env_token")
	result := Resolve("", t.TempDir())
	if result.Token != "env_token" || result.Source != "env" {
		t.Errorf("expected env token, got %+v", result)
	}
}

func TestResolve_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HFETCH_TOKEN", "")
	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"default":"file_token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	result := Resolve("", dir)
	if result.Token != "file_token" || result.Source != "config" {
		t.Errorf("expected config token, got %+v", result)
	}
}

func TestResolve_TrimsWhitespace(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		t.Setenv("HFETCH_TOKEN", "  hf_envtoken\n")
		result := Resolve("", t.TempDir())
		if result.Token != "hf_envtoken" || result.Source != "env" {
			t.Errorf("expected trimmed env token, got %+v", result)
		}
	})

	t.Run("flag", func(t *testing.T) {
		result := Resolve("hf_flagtoken\n", t.TempDir())
		if result.Token != "hf_flagtoken" || result.Source != "flag" {
			t.Errorf("expected trimmed flag token, got %+v", result)
		}
	})

	t.Run("config", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("HFETCH_TOKEN", "")
		if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"default":"hf_filetoken\n"}`), 0600); err != nil {
			t.Fatal(err)
		}
		result := Resolve("", dir)
		if result.Token != "hf_filetoken" || result.Source != "config" {
			t.Errorf("expected trimmed config token, got %+v", result)
		}
	})
}

func TestResolve_None(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HFETCH_TOKEN", "")
	// Override HOME to avoid reading any real HF compat token.
	t.Setenv("HOME", dir)

	result := Resolve("", dir)
	if result.Token != "" || result.Source != "none" {
		t.Errorf("expected no token, got %+v", result)
	}
}

func TestStoreAndClear(t *testing.T) {
	dir := t.TempDir()

	if err := Store("hf_test123", dir); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify the file was created with correct permissions.
	info, err := os.Stat(filepath.Join(dir, "token.json"))
	if err != nil {
		t.Fatalf("token.json not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Resolve should find it.
	t.Setenv("HFETCH_TOKEN", "")
	if result := Resolve("", dir); result.Token != "hf_test123" {
		t.Errorf("expected stored token, got %+v", result)
	}

	// Clear it.
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if result := Resolve("", dir); result.Token != "" {
		t.Errorf("expected no token after clear, got %+v", result)
	}
}

func TestClear_NoFile(t *testing.T) {
	// Should not error when no token file exists.
	if err := Clear(t.TempDir()); err != nil {
		t.Fatalf("Clear on missing file: %v", err)
	}
}
