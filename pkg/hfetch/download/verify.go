package download

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// VerifySHA256 checks that a file matches the expected SHA256 hash.
func VerifySHA256(path, expected string) error {
	if expected == "" {
		return nil // No hash to verify against.
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file for verification: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing file: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expected, got)
	}
	return nil
}
