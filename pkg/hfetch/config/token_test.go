package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveToken_FlagOverride(t *testing.T) {
	t.Setenv("HFETCH_TOKEN", "env_token")
	result := ResolveToken("flag_token")
	if result.Token != "flag_token" || result.Source != "flag" {
		t.Errorf("expected flag override, got %+v", result)
	}
}

func TestResolveToken_EnvVar(t *testing.T) {
	t.Setenv("HFETCH_TOKEN", "env_token")
	result := ResolveToken("")
	if result.Token != "env_token" || result.Source != "env" {
		t.Errorf("expected env token, got %+v", result)
	}
}

func TestResolveToken_ConfigFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_TOKEN", "")
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")

	// Write a token file.
	if err := os.WriteFile(filepath.Join(tmp, "token.json"), []byte(`{"default":"file_token"}`), 0600); err != nil {
		t.Fatal(err)
	}

	result := ResolveToken("")
	if result.Token != "file_token" || result.Source != "config" {
		t.Errorf("expected config token, got %+v", result)
	}
}

func TestResolveToken_None(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_TOKEN", "")
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")
	// Override HOME to avoid reading any real HF compat token.
	t.Setenv("HOME", tmp)

	result := ResolveToken("")
	if result.Token != "" || result.Source != "none" {
		t.Errorf("expected no token, got %+v", result)
	}
}

func TestStoreAndClearToken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")

	if err := StoreToken("hf_test123"); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	// Verify the file was created with correct permissions.
	info, err := os.Stat(filepath.Join(tmp, "token.json"))
	if err != nil {
		t.Fatalf("token.json not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Resolve should find it.
	t.Setenv("HFETCH_TOKEN", "")
	result := ResolveToken("")
	if result.Token != "hf_test123" {
		t.Errorf("expected stored token, got %+v", result)
	}

	// Clear it.
	if err := ClearToken(); err != nil {
		t.Fatalf("ClearToken: %v", err)
	}
	result = ResolveToken("")
	if result.Token != "" {
		t.Errorf("expected no token after clear, got %+v", result)
	}
}

func TestClearToken_NoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")

	// Should not error when no token file exists.
	if err := ClearToken(); err != nil {
		t.Fatalf("ClearToken on missing file: %v", err)
	}
}
