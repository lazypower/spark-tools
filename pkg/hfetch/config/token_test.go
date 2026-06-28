package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The token-resolution behavior suite lives in internal/hftoken. These tests are
// the wiring smoke tests: they prove the public ResolveToken/StoreToken/ClearToken
// thread Dirs().Config (honoring HFETCH_CONFIG_DIR/HFETCH_HOME) into the hftoken
// authority — the integration the config layer owns.

func TestResolveToken_ConfigFileWiring(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_TOKEN", "")
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")

	if err := os.WriteFile(filepath.Join(tmp, "token.json"), []byte(`{"default":"file_token"}`), 0600); err != nil {
		t.Fatal(err)
	}

	result := ResolveToken("")
	if result.Token != "file_token" || result.Source != "config" {
		t.Errorf("ResolveToken must read Dirs().Config/token.json, got %+v", result)
	}
}

func TestStoreAndClearToken_Wiring(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HFETCH_CONFIG_DIR", tmp)
	t.Setenv("HFETCH_HOME", "")

	if err := StoreToken("hf_test123"); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "token.json")); err != nil {
		t.Fatalf("StoreToken must write under Dirs().Config: %v", err)
	}

	t.Setenv("HFETCH_TOKEN", "")
	if result := ResolveToken(""); result.Token != "hf_test123" {
		t.Errorf("expected stored token, got %+v", result)
	}

	if err := ClearToken(); err != nil {
		t.Fatalf("ClearToken: %v", err)
	}
	if result := ResolveToken(""); result.Token != "" {
		t.Errorf("expected no token after clear, got %+v", result)
	}
}
