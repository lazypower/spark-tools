package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/lazypower/spark-tools/pkg/hfetch/auth"
)

// tokenFile is the JSON structure persisted to disk.
type tokenFile struct {
	Default string `json:"default"`
}

// ResolveToken returns the HuggingFace API token using the standard
// resolution order:
//
//	0. Explicit override (--token flag or WithToken())
//	1. HFETCH_TOKEN environment variable
//	2. $HFETCH_CONFIG_DIR/token.json
//	3. HF CLI compatibility path (~/.cache/huggingface/token)
//	4. None
func ResolveToken(override string) auth.TokenResult {
	if override != "" {
		return auth.TokenResult{Token: override, Source: "flag"}
	}

	if v := os.Getenv("HFETCH_TOKEN"); v != "" {
		return auth.TokenResult{Token: v, Source: "env"}
	}

	dirs := Dirs()
	if tok := readTokenFile(filepath.Join(dirs.Config, "token.json")); tok != "" {
		return auth.TokenResult{Token: tok, Source: "config"}
	}

	if tok := readHFCompatToken(); tok != "" {
		return auth.TokenResult{Token: tok, Source: "hf-compat"}
	}

	return auth.TokenResult{Source: "none"}
}

// StoreToken persists a token to $HFETCH_CONFIG_DIR/token.json.
// Creates the config directory (0700) and file (0600) if needed.
func StoreToken(token string) error {
	dirs := Dirs()
	if err := os.MkdirAll(dirs.Config, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(tokenFile{Default: token}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dirs.Config, "token.json"), data, 0600)
}

// ClearToken removes the stored token file.
func ClearToken() error {
	dirs := Dirs()
	path := filepath.Join(dirs.Config, "token.json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readTokenFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var tf tokenFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return ""
	}
	return tf.Default
}

func readHFCompatToken() string {
	// Standard HF CLI token location on Linux/macOS.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".cache", "huggingface", "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
