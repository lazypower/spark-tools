package config

import (
	"github.com/lazypower/spark-tools/internal/hftoken"
	"github.com/lazypower/spark-tools/pkg/hfetch/auth"
)

// The token resolution/persistence logic moved to internal/hftoken during the
// /internal extraction. These functions stay here as the stable public surface
// (cmd/hfetch and pkg/hfetch import them) and supply the one thing hftoken must
// not resolve itself — the config directory — by passing Dirs().Config down.
// Keeping Dirs() resolution here is what breaks the config<->hftoken cycle.

// ResolveToken returns the HuggingFace API token using the standard resolution
// order: override, HFETCH_TOKEN, $HFETCH_CONFIG_DIR/token.json, the HF CLI compat
// path, then none.
func ResolveToken(override string) auth.TokenResult {
	return hftoken.Resolve(override, Dirs().Config)
}

// StoreToken persists a token to $HFETCH_CONFIG_DIR/token.json (dir 0700, file
// 0600).
func StoreToken(token string) error {
	return hftoken.Store(token, Dirs().Config)
}

// ClearToken removes the stored token file.
func ClearToken() error {
	return hftoken.Clear(Dirs().Config)
}
