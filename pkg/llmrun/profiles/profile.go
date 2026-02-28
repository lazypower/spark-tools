// Package profiles manages saved inference configurations (named RunConfigs).
package profiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lazypower/spark-tools/pkg/llmrun/engine"
)

// Profile is a named, saved RunConfig.
type Profile struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Config      engine.RunConfig `json:"config"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	LastUsedAt  time.Time        `json:"lastUsedAt,omitempty"`
}

// ProfileStore manages saved profiles on disk.
type ProfileStore struct {
	configDir string
}

// NewProfileStore creates a ProfileStore that reads/writes profiles
// under configDir/profiles/.
func NewProfileStore(configDir string) *ProfileStore {
	return &ProfileStore{configDir: configDir}
}

// profilesDir returns the directory where profile JSON files are stored.
func (ps *ProfileStore) profilesDir() string {
	return filepath.Join(ps.configDir, "profiles")
}

// profilePath returns the file path for a named profile.
func (ps *ProfileStore) profilePath(name string) string {
	return filepath.Join(ps.profilesDir(), name+".json")
}

// List returns all saved profiles from disk. Built-in profiles are
// included only if no on-disk override exists for them.
func (ps *ProfileStore) List() ([]Profile, error) {
	dir := ps.profilesDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		// No profiles directory; return just the built-ins.
		return BuiltinProfiles(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	seen := make(map[string]bool)
	var profiles []Profile

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading profile %s: %w", entry.Name(), err)
		}
		var p Profile
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing profile %s: %w", entry.Name(), err)
		}
		seen[p.Name] = true
		profiles = append(profiles, p)
	}

	// Append built-in profiles that aren't overridden on disk.
	for _, bp := range BuiltinProfiles() {
		if !seen[bp.Name] {
			profiles = append(profiles, bp)
		}
	}

	return profiles, nil
}

// Get returns a profile by name. It checks on-disk profiles first,
// then falls back to built-in profiles.
func (ps *ProfileStore) Get(name string) (*Profile, error) {
	data, err := os.ReadFile(ps.profilePath(name))
	if err == nil {
		var p Profile
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parsing profile %q: %w", name, err)
		}
		return &p, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading profile %q: %w", name, err)
	}

	// Check built-in profiles.
	for _, bp := range BuiltinProfiles() {
		if bp.Name == name {
			return &bp, nil
		}
	}

	return nil, fmt.Errorf("profile %q not found", name)
}

// Save writes a profile to disk as JSON. It creates the profiles
// directory if it doesn't exist and updates UpdatedAt.
func (ps *ProfileStore) Save(p Profile) error {
	if p.Name == "" {
		return fmt.Errorf("profile name must not be empty")
	}

	dir := ps.profilesDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating profiles directory: %w", err)
	}

	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling profile %q: %w", p.Name, err)
	}

	if err := os.WriteFile(ps.profilePath(p.Name), data, 0644); err != nil {
		return fmt.Errorf("writing profile %q: %w", p.Name, err)
	}
	return nil
}

// Delete removes a profile from disk.
func (ps *ProfileStore) Delete(name string) error {
	err := os.Remove(ps.profilePath(name))
	if os.IsNotExist(err) {
		return fmt.Errorf("profile %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("deleting profile %q: %w", name, err)
	}
	return nil
}
