// Package config handles XDG directory resolution, token resolution,
// and persistent configuration for the hfetch toolchain.
//
// This package reads from disk and environment only — no network calls,
// no API dependency. Safe to import from any package without pulling in
// HTTP clients.
package config
