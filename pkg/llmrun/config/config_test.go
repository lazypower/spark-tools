package config

import (
	"testing"

	rc "github.com/lazypower/spark-tools/internal/runconfig"
)

// The behavior suite (XDG precedence, env overrides, save/load round trip) lives
// in internal/runconfig; this locks the compat surface (alias identity, delegated
// funcs).

func TestWrapper_AliasIdentity(t *testing.T) {
	var _ rc.DirConfig = DirConfig{}
	var _ rc.GlobalConfig = GlobalConfig{}
}

func TestWrapper_DelegatesDefaultsAndDirs(t *testing.T) {
	if DefaultGlobalConfig() != rc.DefaultGlobalConfig() {
		t.Error("DefaultGlobalConfig must delegate to the authority")
	}
	if Dirs() != rc.Dirs() {
		t.Error("Dirs must delegate to the authority")
	}
}

func TestWrapper_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LLM_RUN_HOME", dir)
	cfg := DefaultGlobalConfig()
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := LoadGlobalConfig(); got != cfg {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, cfg)
	}
}
