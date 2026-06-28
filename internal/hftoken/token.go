// Package hftoken resolves and persists the HuggingFace API token. The config
// directory is INJECTED by the caller (pkg/hfetch/config passes Dirs().Config),
// so this package does not import config — which is what breaks the otherwise
// unavoidable config<->hftoken import cycle (config owns Dirs(); the token logic
// needs the config dir). The auth.TokenResult vocabulary and source labels stay
// canonical in pkg/hfetch/auth; this package never redefines them.
package hftoken

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

// Resolve returns the HuggingFace API token using the standard resolution order:
//
//	0. Explicit override (--token flag or WithToken())
//	1. HFETCH_TOKEN environment variable
//	2. <configDir>/token.json
//	3. HF CLI compatibility path (~/.cache/huggingface/token)
//	4. None
//
// configDir is the resolved hfetch config directory (caller-injected to avoid a
// dependency on the config package).
func Resolve(override, configDir string) auth.TokenResult {
	// Trim every source: a stray newline or space from a paste produces an
	// invalid `Authorization: Bearer <token>\n` header and a confusing 401.
	if override = strings.TrimSpace(override); override != "" {
		return auth.TokenResult{Token: override, Source: "flag"}
	}

	if v := strings.TrimSpace(os.Getenv("HFETCH_TOKEN")); v != "" {
		return auth.TokenResult{Token: v, Source: "env"}
	}

	if tok := readTokenFile(filepath.Join(configDir, "token.json")); tok != "" {
		return auth.TokenResult{Token: tok, Source: "config"}
	}

	if tok := readHFCompatToken(); tok != "" {
		return auth.TokenResult{Token: tok, Source: "hf-compat"}
	}

	return auth.TokenResult{Source: "none"}
}

// Store persists a token to <configDir>/token.json. Creates the config
// directory (0700) and file (0600) if needed.
func Store(token, configDir string) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(tokenFile{Default: token}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, "token.json"), data, 0600)
}

// Clear removes the stored token file under configDir.
func Clear(configDir string) error {
	path := filepath.Join(configDir, "token.json")
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
	return strings.TrimSpace(tf.Default)
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
