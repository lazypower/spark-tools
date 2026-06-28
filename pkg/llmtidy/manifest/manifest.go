// Package manifest is a compatibility wrapper over internal/tidymanifest. The
// llm-tidy desired-state manifest authority moved to internal/tidymanifest during
// the /internal extraction; this thin alias keeps existing importers (pkg/llmtidy,
// pkg/llmtidy/reconcile, cmd/llm-tidy) compiling unchanged until they migrate.
//
// Deprecated: import github.com/lazypower/spark-tools/internal/tidymanifest.
package manifest

import "github.com/lazypower/spark-tools/internal/tidymanifest"

// Schema / filename / env constants (re-exported).
const (
	SchemaVersion   = tidymanifest.SchemaVersion
	DefaultFilename = tidymanifest.DefaultFilename
	EnvManifest     = tidymanifest.EnvManifest
	EnvConfigDir    = tidymanifest.EnvConfigDir
	EnvXDGConfig    = tidymanifest.EnvXDGConfig
	AppName         = tidymanifest.AppName
)

// ErrNotFound is the same sentinel as the authority, so errors.Is keeps working
// across the wrapper boundary.
var ErrNotFound = tidymanifest.ErrNotFound

// Manifest types (aliases — methods like OllamaModelSpec.NormalizedName carry over).
type (
	Manifest        = tidymanifest.Manifest
	OllamaModelSpec = tidymanifest.OllamaModelSpec
	GGUFModelSpec   = tidymanifest.GGUFModelSpec
	VLLMModelSpec   = tidymanifest.VLLMModelSpec
)

// Resolve picks the manifest path. Delegates to the tidymanifest authority.
func Resolve(flagPath string) (string, error) { return tidymanifest.Resolve(flagPath) }

// ConfigDir returns the resolved config directory.
func ConfigDir() (string, error) { return tidymanifest.ConfigDir() }

// NormalizeOllamaName appends ":latest" when no tag is present.
func NormalizeOllamaName(name string) string { return tidymanifest.NormalizeOllamaName(name) }

// Load reads and parses a manifest, returning ErrNotFound when absent.
func Load(path string) (*Manifest, error) { return tidymanifest.Load(path) }

// Save writes the manifest as YAML to the given path.
func Save(m *Manifest, path string) error { return tidymanifest.Save(m, path) }

// Validate checks the manifest for structural errors.
func Validate(m *Manifest) error { return tidymanifest.Validate(m) }
