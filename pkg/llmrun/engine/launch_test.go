package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Different ports must use different PID files, so chat/serve/run/bench on
// distinct ports no longer collide on one global server.pid. An unset port
// resolves to the single default.
func TestPIDFilePath_KeyedByPort(t *testing.T) {
	dir := "/data"
	if a, b := pidFilePath(dir, RunConfig{Port: 8080}), pidFilePath(dir, RunConfig{Port: 9090}); a == b {
		t.Fatalf("different ports must use different PID files: %s == %s", a, b)
	}
	if got, want := pidFilePath(dir, RunConfig{}), pidFilePath(dir, RunConfig{Port: defaultServerPort}); got != want {
		t.Errorf("unset port must resolve to the default: %s != %s", got, want)
	}
}

func TestCheckPIDFile_NoFileIsOK(t *testing.T) {
	pf := filepath.Join(t.TempDir(), "server-8080.pid")
	if err := checkPIDFile(pf, RunConfig{Port: 8080}); err != nil {
		t.Errorf("a missing PID file must be OK, got %v", err)
	}
}

func TestCheckPIDFile_StaleAndCorruptAreCleaned(t *testing.T) {
	dir := t.TempDir()

	corrupt := filepath.Join(dir, "server-8080.pid")
	if err := os.WriteFile(corrupt, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkPIDFile(corrupt, RunConfig{Port: 8080}); err != nil {
		t.Errorf("a corrupt PID file must be cleaned, got %v", err)
	}
	if _, err := os.Stat(corrupt); !os.IsNotExist(err) {
		t.Error("a corrupt PID file must be removed")
	}

	// A PID far above the valid range is guaranteed dead.
	stale := filepath.Join(dir, "server-9090.pid")
	if err := os.WriteFile(stale, []byte("2147483646"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkPIDFile(stale, RunConfig{Port: 9090}); err != nil {
		t.Errorf("a stale PID file must be cleaned, got %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale PID file must be removed")
	}
}

// A live process must produce a conflict error that names the in-use port —
// with per-port PID files the colliding server is by definition on this port.
func TestCheckPIDFile_LiveProcessConflictsOnItsPort(t *testing.T) {
	pf := filepath.Join(t.TempDir(), "server-8080.pid")
	if err := os.WriteFile(pf, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkPIDFile(pf, RunConfig{Port: 8080})
	if err == nil {
		t.Fatal("a live process must produce a conflict error")
	}
	if !strings.Contains(err.Error(), "port 8080") {
		t.Errorf("the conflict error must name the in-use port, got: %v", err)
	}
	// A live-process conflict must NOT delete the PID file.
	if _, statErr := os.Stat(pf); statErr != nil {
		t.Error("a live-process PID file must be preserved")
	}
}
