// Package auth defines shared authentication types and sentinel errors
// used across the spark-tools toolchain. It has no network or API
// dependencies — safe to import from any package.
package auth

import "errors"

var (
	// ErrAuthRequired is returned when an operation requires authentication
	// but no token is configured.
	ErrAuthRequired = errors.New("hfetch: authentication required — run `hfetch login`")

	// ErrAuthInvalid is returned when the configured token is rejected by
	// the HuggingFace API (expired, revoked, malformed).
	ErrAuthInvalid = errors.New("hfetch: token is invalid — run `hfetch login` to re-authenticate")

	// ErrGatedModel is returned when the user is authenticated but has not
	// accepted the model's terms on the HuggingFace website.
	ErrGatedModel = errors.New("hfetch: model requires license acceptance at huggingface.co")
)
