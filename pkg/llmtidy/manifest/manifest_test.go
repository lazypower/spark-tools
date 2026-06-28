package manifest

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/internal/tidymanifest"
)

// The full behavior suite lives in internal/tidymanifest. These lock only the
// compatibility surface: delegation, alias identity, and sentinel-error identity.

func TestWrapper_LoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := Save(&Manifest{GGUF: []GGUFModelSpec{{Repo: "org/m", Quant: "Q4_K_M"}}}, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m, err := Load(path)
	if err != nil || len(m.GGUF) != 1 || m.GGUF[0].Repo != "org/m" {
		t.Fatalf("round-trip via wrapper failed: %+v err=%v", m, err)
	}
}

func TestWrapper_ErrNotFoundIdentity(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, tidymanifest.ErrNotFound) {
		t.Errorf("wrapper ErrNotFound must be the same sentinel as the authority, got %v", err)
	}
}

func TestWrapper_TypeAliasIdentity(t *testing.T) {
	var _ *tidymanifest.Manifest = (*Manifest)(nil)
	if NormalizeOllamaName("llama3") != tidymanifest.NormalizeOllamaName("llama3") {
		t.Error("NormalizeOllamaName must delegate to the authority")
	}
}
