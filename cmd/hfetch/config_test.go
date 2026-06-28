package main

import (
	"testing"
)

func TestPrefs_RoundTrip(t *testing.T) {
	t.Setenv("HFETCH_CONFIG_DIR", t.TempDir())

	// Loading before anything is written is an error (no file).
	if _, err := loadPrefs(); err == nil {
		t.Error("loadPrefs on a fresh config dir must error (no file yet)")
	}

	if err := savePrefs(map[string]any{"token_source": "env", "streams": "8"}); err != nil {
		t.Fatalf("savePrefs: %v", err)
	}
	got, err := loadPrefs()
	if err != nil {
		t.Fatalf("loadPrefs: %v", err)
	}
	if got["token_source"] != "env" || got["streams"] != "8" {
		t.Errorf("prefs did not round-trip, got %+v", got)
	}
}

func TestConfigSetGet_Persist(t *testing.T) {
	t.Setenv("HFETCH_CONFIG_DIR", t.TempDir())

	set := configSetCmd()
	set.SetArgs([]string{"default_quant", "Q4_K_M"})
	if err := set.Execute(); err != nil {
		t.Fatalf("config set: %v", err)
	}

	// The value persisted to disk.
	prefs, err := loadPrefs()
	if err != nil || prefs["default_quant"] != "Q4_K_M" {
		t.Fatalf("set did not persist: prefs=%+v err=%v", prefs, err)
	}

	// get on a missing key errors.
	get := configGetCmd()
	get.SetArgs([]string{"nonexistent"})
	if err := get.Execute(); err == nil {
		t.Error("config get on an unset key must error")
	}
}
