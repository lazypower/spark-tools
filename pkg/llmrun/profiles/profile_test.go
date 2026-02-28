package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

func TestBuiltinProfiles(t *testing.T) {
	builtins := BuiltinProfiles()

	if len(builtins) != 4 {
		t.Fatalf("expected 4 built-in profiles, got %d", len(builtins))
	}

	names := make(map[string]Profile)
	for _, p := range builtins {
		names[p.Name] = p
	}

	// Check each expected built-in.
	tests := []struct {
		name        string
		desc        string
		temp        float64
		topP        float64
		contextSize int
	}{
		{"default", "General-purpose conversation", 0.7, 0.0, 0},
		{"coding", "Code generation", 0.3, 0.9, 0},
		{"creative", "Creative writing", 0.9, 0.95, 0},
		{"precise", "Factual/analytical", 0.1, 0.8, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := names[tt.name]
			if !ok {
				t.Fatalf("built-in profile %q not found", tt.name)
			}
			if p.Description != tt.desc {
				t.Errorf("description = %q, want %q", p.Description, tt.desc)
			}
			if p.Config.Temperature != tt.temp {
				t.Errorf("temperature = %v, want %v", p.Config.Temperature, tt.temp)
			}
			if p.Config.TopP != tt.topP {
				t.Errorf("topP = %v, want %v", p.Config.TopP, tt.topP)
			}
			if p.Config.ContextSize != tt.contextSize {
				t.Errorf("contextSize = %v, want %v", p.Config.ContextSize, tt.contextSize)
			}
		})
	}
}

func TestProfileStore_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	p := Profile{
		Name:        "test-profile",
		Description: "A test profile",
		Config: engine.RunConfig{
			Temperature: 0.5,
			TopP:        0.85,
			ContextSize: 4096,
		},
	}

	// Save the profile.
	if err := store.Save(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists on disk.
	fp := filepath.Join(dir, "profiles", "test-profile.json")
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("profile file not created: %v", err)
	}

	// Get it back.
	got, err := store.Get("test-profile")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Name != p.Name {
		t.Errorf("name = %q, want %q", got.Name, p.Name)
	}
	if got.Description != p.Description {
		t.Errorf("description = %q, want %q", got.Description, p.Description)
	}
	if got.Config.Temperature != p.Config.Temperature {
		t.Errorf("temperature = %v, want %v", got.Config.Temperature, p.Config.Temperature)
	}
	if got.Config.TopP != p.Config.TopP {
		t.Errorf("topP = %v, want %v", got.Config.TopP, p.Config.TopP)
	}
	if got.Config.ContextSize != p.Config.ContextSize {
		t.Errorf("contextSize = %v, want %v", got.Config.ContextSize, p.Config.ContextSize)
	}
	if got.CreatedAt.IsZero() {
		t.Error("createdAt should be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("updatedAt should be set")
	}
}

func TestProfileStore_SaveUpdatesUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	p := Profile{
		Name:        "evolving",
		Description: "First version",
		Config:      engine.RunConfig{Temperature: 0.5},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}

	first, err := store.Get("evolving")
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// Save again with updated description.
	p.Description = "Second version"
	p.CreatedAt = first.CreatedAt // Preserve the original creation time.
	if err := store.Save(p); err != nil {
		t.Fatalf("second Save failed: %v", err)
	}

	second, err := store.Get("evolving")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}

	if second.Description != "Second version" {
		t.Errorf("description = %q, want %q", second.Description, "Second version")
	}
	if !second.UpdatedAt.After(first.UpdatedAt) && second.UpdatedAt != first.UpdatedAt {
		t.Error("updatedAt should be >= first save")
	}
}

func TestProfileStore_SaveEmptyNameError(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	err := store.Save(Profile{})
	if err == nil {
		t.Fatal("expected error for empty profile name")
	}
}

func TestProfileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	p := Profile{
		Name:   "deleteme",
		Config: engine.RunConfig{Temperature: 0.5},
	}
	if err := store.Save(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it exists.
	if _, err := store.Get("deleteme"); err != nil {
		t.Fatalf("Get before delete failed: %v", err)
	}

	// Delete it.
	if err := store.Delete("deleteme"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone from disk (it shouldn't fall back to a built-in
	// since "deleteme" isn't a built-in name).
	_, err := store.Get("deleteme")
	if err == nil {
		t.Fatal("expected error after deleting profile")
	}
}

func TestProfileStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	err := store.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent profile")
	}
}

func TestProfileStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	// With no on-disk profiles, List should return the built-ins.
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(profiles) != 4 {
		t.Fatalf("expected 4 profiles (built-ins), got %d", len(profiles))
	}

	// Save a custom profile.
	custom := Profile{
		Name:   "custom",
		Config: engine.RunConfig{Temperature: 0.42},
	}
	if err := store.Save(custom); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	profiles, err = store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// 1 on-disk + 4 built-ins = 5
	if len(profiles) != 5 {
		t.Fatalf("expected 5 profiles, got %d", len(profiles))
	}
}

func TestProfileStore_ListOverrideBuiltin(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	// Override the "coding" built-in.
	override := Profile{
		Name:        "coding",
		Description: "My custom coding profile",
		Config:      engine.RunConfig{Temperature: 0.2},
	}
	if err := store.Save(override); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// 1 on-disk (overrides "coding") + 3 remaining built-ins = 4
	if len(profiles) != 4 {
		t.Fatalf("expected 4 profiles, got %d", len(profiles))
	}

	// The "coding" entry should be our override.
	for _, p := range profiles {
		if p.Name == "coding" {
			if p.Description != "My custom coding profile" {
				t.Errorf("expected overridden description, got %q", p.Description)
			}
			if p.Config.Temperature != 0.2 {
				t.Errorf("expected overridden temperature 0.2, got %v", p.Config.Temperature)
			}
			return
		}
	}
	t.Fatal("coding profile not found in list")
}

func TestProfileStore_GetBuiltinFallback(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	// Get a built-in profile without saving it to disk.
	p, err := store.Get("default")
	if err != nil {
		t.Fatalf("Get built-in failed: %v", err)
	}
	if p.Name != "default" {
		t.Errorf("name = %q, want %q", p.Name, "default")
	}
	if p.Description != "General-purpose conversation" {
		t.Errorf("description = %q, want %q", p.Description, "General-purpose conversation")
	}
}

func TestProfileStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewProfileStore(dir)

	_, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}
