package serveinstance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/internal/fingerprint"
	"github.com/lazypower/spark-tools/internal/serving"
)

func sample(name string) Instance {
	return Instance{
		Desired: Desired{
			Name:          name,
			ServedName:    "qwen-36b-fp4",
			ModelID:       "Qwen/Qwen3.6-35B-A3B-NVFP4",
			ModelRevision: "abc123",
			ModelDir:      "/srv/models/Qwen3.6-35B-A3B-NVFP4",
			ContractKey:   serving.ContractKey{Arch: "Qwen3MoeForCausalLM", Quant: serving.QuantNVFP4},
			SpecPath:      "/var/lib/llm-serve/" + name + "/compose.yml",
			SpecHash:      "deadbeef",
			Target:        fingerprint.Fingerprint{Engine: "vllm/vllm-openai@v0.23.0", Accelerator: "nvidia:gb10:sm121"},
			ProjectName:   "llm-serve-" + name,
		},
	}
}

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	in := sample("qwen")
	if err := s.Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Load("qwen")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Desired.Name != "qwen" || got.Desired.SpecHash != "deadbeef" ||
		got.Desired.ContractKey.Quant != serving.QuantNVFP4 ||
		got.Desired.Target.Engine != "vllm/vllm-openai@v0.23.0" {
		t.Errorf("round-trip mismatch: %+v", got.Desired)
	}
	if got.Operation != nil {
		t.Error("steady manifest must have no operation")
	}
}

func TestStore_LoadMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Load("nope"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_ListSkipsLockAndTemp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Save(sample("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sample("b")); err != nil {
		t.Fatal(err)
	}
	// Take the lock (creates .lock) and drop a stray temp file.
	unlock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.WriteFile(filepath.Join(dir, tmpPrefix+"junk-123"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 manifests (lock + temp skipped), got %d", len(list))
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Save(sample("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Load("x"); err != ErrNotFound {
		t.Error("manifest should be gone after delete")
	}
	if err := s.Delete("x"); err != nil {
		t.Errorf("deleting a missing manifest must be idempotent, got %v", err)
	}
}

func TestStore_AtomicSave_NoPartialOnList(t *testing.T) {
	// After a successful Save there must be exactly one manifest file and no
	// lingering temp file (the rename consumed it).
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Save(sample("q")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != manifestSuffix {
			t.Errorf("unexpected leftover file after atomic save: %q", e.Name())
		}
	}
}

func TestStore_RejectsUnsafeNames(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, bad := range []string{"", ".", "..", "a/b", "../escape", ".hidden", "a\\b"} {
		in := sample("ok")
		in.Desired.Name = bad
		if err := s.Save(in); err == nil {
			t.Errorf("Save must reject unsafe name %q", bad)
		}
		if _, err := s.Load(bad); err == nil {
			t.Errorf("Load must reject unsafe name %q", bad)
		}
	}
}

func TestStore_RejectsInvalidPhase(t *testing.T) {
	s := NewStore(t.TempDir())
	in := sample("p")
	in.Operation = &Operation{Phase: Phase("frobnicate")}
	if err := s.Save(in); err == nil {
		t.Error("Save must reject an unknown operation phase")
	}
}

func TestStore_LockRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	unlock, err := s.Lock()
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := unlock(); err != nil {
		t.Errorf("unlock: %v", err)
	}
	// Re-acquire after release must succeed.
	unlock2, err := s.Lock()
	if err != nil {
		t.Fatalf("re-lock after release: %v", err)
	}
	unlock2()
}

func TestInstance_InFlight(t *testing.T) {
	in := sample("f")
	if in.InFlight() {
		t.Error("steady manifest is not in-flight")
	}
	in.Operation = &Operation{Phase: PhaseStarting}
	if !in.InFlight() {
		t.Error("starting manifest is in-flight")
	}
}
