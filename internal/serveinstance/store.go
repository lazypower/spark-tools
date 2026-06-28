package serveinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrNotFound is returned by Load when no manifest exists for a name.
var ErrNotFound = errors.New("instance manifest not found")

// Store persists instance manifests under a directory, one file per instance
// (<name>.json). Saves are atomic (temp + rename). A single host lock
// (Store.Lock) serializes mutations and recovery; reads (Load/List) are pure
// snapshots and take no lock.
type Store struct {
	dir string
}

// NewStore returns a Store backed by dir (created on first write).
func NewStore(dir string) *Store { return &Store{dir: dir} }

const (
	manifestSuffix = ".json"
	lockFile       = ".lock"
	tmpPrefix      = ".tmp-"
)

// ValidName reports whether name is safe to use as a manifest filename: a manifest
// is named by the instance Name, so an unsafe name (path separators, traversal,
// dotfiles) must be rejected at the boundary rather than escape the store dir.
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return false
	}
	return name == filepath.Base(name)
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+manifestSuffix)
}

// Save atomically writes a manifest. It validates the instance name, writes to a
// temp file in the same directory, then renames over the target so a reader never
// observes a partial manifest.
func (s *Store) Save(inst Instance) error {
	if !ValidName(inst.Desired.Name) {
		return fmt.Errorf("invalid instance name %q", inst.Desired.Name)
	}
	if inst.Operation != nil && !inst.Operation.Phase.Valid() {
		return fmt.Errorf("invalid operation phase %q", inst.Operation.Phase)
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("creating instance dir: %w", err)
	}
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	tmp, err := os.CreateTemp(s.dir, tmpPrefix+inst.Desired.Name+"-*")
	if err != nil {
		return fmt.Errorf("creating temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, s.path(inst.Desired.Name)); err != nil {
		return fmt.Errorf("renaming manifest into place: %w", err)
	}
	return nil
}

// Load returns the manifest for a name, or ErrNotFound.
func (s *Store) Load(name string) (*Instance, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid instance name %q", name)
	}
	data, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading manifest %q: %w", name, err)
	}
	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("parsing manifest %q: %w", name, err)
	}
	return &inst, nil
}

// List returns all manifests, skipping the lock file and any in-progress temp
// writes. It is a pure snapshot — callers that need recovery must take the lock
// separately.
func (s *Store) List() ([]Instance, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing instance dir: %w", err)
	}
	var out []Instance
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, manifestSuffix) || strings.HasPrefix(n, tmpPrefix) {
			continue
		}
		inst, err := s.Load(strings.TrimSuffix(n, manifestSuffix))
		if err != nil {
			return nil, err
		}
		out = append(out, *inst)
	}
	return out, nil
}

// Delete removes a manifest. Removing a missing manifest is not an error
// (teardown is idempotent). Callers must only delete on CONFIRMED runtime absence
// — an unconfirmed teardown keeps the manifest as a recovery handle instead.
func (s *Store) Delete(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid instance name %q", name)
	}
	err := os.Remove(s.path(name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting manifest %q: %w", name, err)
	}
	return nil
}

// Lock acquires the exclusive host lock that serializes mutations and recovery
// across processes on this host. It blocks until the lock is held and returns an
// unlock function. Reads do not take this lock.
func (s *Store) Lock() (unlock func() error, err error) {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, fmt.Errorf("creating instance dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(s.dir, lockFile), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquiring host lock: %w", err)
	}
	return func() error {
		defer f.Close()
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
