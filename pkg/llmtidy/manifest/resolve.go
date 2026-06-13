package manifest

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	// DefaultFilename is the on-disk name of the manifest file.
	DefaultFilename = "manifest.yaml"

	// EnvManifest, when set, is treated as an explicit manifest path.
	EnvManifest = "LLM_TIDY_MANIFEST"

	// EnvConfigDir, when set, holds the directory the manifest lives in.
	EnvConfigDir = "LLM_TIDY_CONFIG_DIR"

	// EnvXDGConfig is the XDG base-directory variable for config.
	EnvXDGConfig = "XDG_CONFIG_HOME"

	// AppName is the directory name used under XDG_CONFIG_HOME.
	AppName = "llm-tidy"
)

// Resolve picks the manifest path per spec §4.4:
//
//  1. flagPath if non-empty
//  2. $LLM_TIDY_MANIFEST
//  3. $LLM_TIDY_CONFIG_DIR/manifest.yaml
//  4. ${XDG_CONFIG_HOME:-$HOME/.config}/llm-tidy/manifest.yaml
//
// Existence is not checked. Use Load to read and detect ErrNotFound.
func Resolve(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if v := os.Getenv(EnvManifest); v != "" {
		return v, nil
	}
	if v := os.Getenv(EnvConfigDir); v != "" {
		return filepath.Join(v, DefaultFilename), nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultFilename), nil
}

// ConfigDir returns the resolved config directory, honoring LLM_TIDY_CONFIG_DIR
// then XDG_CONFIG_HOME, then ~/.config/llm-tidy.
func ConfigDir() (string, error) {
	if v := os.Getenv(EnvConfigDir); v != "" {
		return v, nil
	}
	if v := os.Getenv(EnvXDGConfig); v != "" {
		return filepath.Join(v, AppName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot resolve user home directory")
	}
	return filepath.Join(home, ".config", AppName), nil
}
