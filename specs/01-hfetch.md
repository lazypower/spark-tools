# hfetch — HuggingFace Client in Pure Go

**Status:** Draft
**Created:** 2026-02-27
**Component:** `hfetch`
**Language:** Go (pure — zero cgo, zero Python)

---

## 1. Problem Statement

Working with HuggingFace models today requires navigating Python dependency hell: `huggingface_hub`, `transformers`, `safetensors`, and their transitive dependency graphs. For a workflow centered on a DGX Spark GB10 running llama.cpp close to the metal, a Python runtime is an unnecessary layer of friction. Every model download, every repo inspection, every file listing currently requires bootstrapping a Python environment or shelling out to `huggingface-cli`.

We need a self-contained Go binary that speaks the HuggingFace Hub API natively, handles the GGUF-centric workflow that llama.cpp demands, and integrates cleanly as both a CLI tool and an importable Go library.

## 2. Goals

1. **Zero external runtime dependencies.** Single static binary. No Python, no Node, no containers.
2. **First-class GGUF awareness.** Understand quantization variants (Q4_K_M, Q5_K_S, Q8_0, etc.), parse GGUF metadata headers, and present model options in terms a llama.cpp user thinks in.
3. **Resumable, parallel downloads.** Models are 10–120 GB. Downloads must survive interruption and saturate available bandwidth.
4. **Library-first design.** The CLI is a thin shell over a Go package. Other tools (the llama.cpp wrapper, the benchmark suite) import the library directly.
5. **Offline-capable model registry.** Once downloaded, models are tracked in a local manifest. No network required to list, inspect, or locate local models.

## 3. Non-Goals

- Training, fine-tuning, or inference. This is a model logistics tool.
- Supporting every HuggingFace API endpoint. Focus on model discovery, download, and metadata.
- Replicating the full `huggingface_hub` Python library surface area.
- Safetensors/PyTorch weight conversion. Out of scope — we consume GGUF directly.

## 4. Architecture

### 4.1 Package Structure

```
hfetch/
├── cmd/hfetch/          # CLI entrypoint
├── pkg/
│   ├── api/             # HuggingFace Hub REST API client
│   │   ├── client.go    # HTTP client, auth, rate limiting
│   │   ├── models.go    # /api/models endpoints
│   │   ├── repos.go     # /api/repos endpoints
│   │   ├── auth.go      # WhoAmI, token validation (network-dependent)
│   │   └── types.go     # API response types
│   ├── auth/            # Auth errors and types (no network, no API dependency)
│   │   ├── errors.go    # ErrAuthRequired, ErrAuthInvalid, ErrGatedModel
│   │   └── types.go     # UserInfo, TokenResult
│   ├── registry/        # Local model registry
│   │   ├── manifest.go  # JSON manifest for downloaded models
│   │   ├── storage.go   # Disk layout, path resolution
│   │   └── gc.go        # Garbage collection for partial/orphaned downloads
│   ├── download/        # Download engine
│   │   ├── manager.go   # Orchestrates parallel, resumable downloads
│   │   ├── chunk.go     # Range-request based chunked downloads
│   │   └── verify.go    # SHA256 verification against HF-provided hashes
│   ├── gguf/            # GGUF format awareness
│   │   ├── parser.go    # Parse GGUF file headers (metadata only, not weights)
│   │   ├── types.go     # Quantization types, architecture metadata
│   │   └── filter.go    # Filter/rank files by quantization, size, architecture
│   └── config/          # Configuration and token resolution (no network)
│       ├── config.go    # XDG directory resolution, preferences
│       ├── dirs.go      # XDG base directory logic
│       └── token.go     # Token resolution and storage (disk + env only)
└── internal/
    ├── progress/        # Terminal progress bars and download status
    └── ui/              # Interactive terminal components (charmbracelet/huh)
        ├── picker.go    # GGUF file selection picker
        └── confirm.go   # Download confirmation prompts
```

### 4.2 Terminal UI Stack

All interactive terminal components use the [Charm](https://charm.sh) library family:

- **[`charmbracelet/huh`](https://github.com/charmbracelet/huh)** — selection pickers, confirmations, text input (e.g., file picker, login token prompt). Built on bubbletea but purpose-built for form-style interactions without needing a full TUI model.
- **[`charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss)** — styling for tables, progress output, box drawing. Shared across the toolchain for visual consistency.

This is the same Charm stack used by llm-run and llm-bench, giving all three tools a consistent look and feel with zero cgo.

### 4.3 Core Types

```go
// Model represents a HuggingFace model repository
type Model struct {
    ID          string            // e.g. "TheBloke/Llama-2-70B-GGUF"
    Author      string
    Tags        []string
    Downloads   int
    LastUpdated time.Time
    Files       []ModelFile
}

// ModelFile represents a single file in a model repo
type ModelFile struct {
    Filename     string
    Size         int64
    SHA256       string
    LFS          bool              // Whether file is in Git LFS
    Quantization string            // Parsed from filename or GGUF header (e.g. "Q4_K_M")
    GGUFMeta     *GGUFMetadata     // Populated after header parse, nil for non-GGUF
}

// GGUFMetadata contains parsed GGUF header information
type GGUFMetadata struct {
    Architecture    string          // e.g. "llama", "mistral", "qwen2"
    ParameterCount  int64
    ContextLength   int
    QuantType       string
    FileType        int
    HeadCount       int
    LayerCount      int
    VocabSize       int
    CustomMetadata  map[string]any  // Catch-all for model-specific metadata
}

// LocalModel represents a downloaded model in the local registry
type LocalModel struct {
    Model
    LocalPath    string
    DownloadedAt time.Time
    Verified     bool              // SHA256 verified post-download
    Files        []LocalFile
}

// LocalFile is a ModelFile that exists on disk
type LocalFile struct {
    ModelFile
    LocalPath string
    Complete  bool
}

// Manifest is the on-disk registry of all downloaded models.
// Stored at $HFETCH_DATA_DIR/manifest.json.
type Manifest struct {
    SchemaVersion int            `json:"schema_version"` // Currently 1. Bump on breaking changes.
    Models        []LocalModel   `json:"models"`
}
```

### 4.4 Storage Layout

hfetch follows the XDG Base Directory Specification, separating config, data, and cache:

```
HFETCH_CONFIG_DIR/                     # Default: ~/.config/hfetch
├── config.json                        # Preferences, settings
└── token.json                         # Auth token (0600 permissions)

HFETCH_DATA_DIR/                       # Default: ~/.local/share/hfetch
├── manifest.json                      # Registry of all downloaded models (schema_version: 1)
└── models/
    └── <org>--<model>/                # e.g. TheBloke--Llama-2-70B-GGUF
        ├── <filename>.gguf            # The actual model file
        └── <filename>.gguf.partial    # In-progress download state

HFETCH_CACHE_DIR/                      # Default: ~/.cache/hfetch
└── models/
    └── <org>--<model>/
        └── meta.json                  # Cached API metadata (rebuildable)
```

**Directory override rules:**
- `HFETCH_HOME` is a convenience shortcut. If set, it remaps all three:
  - `$HFETCH_HOME/config/` → config dir
  - `$HFETCH_HOME/data/` → data dir
  - `$HFETCH_HOME/cache/` → cache dir
- Individual `HFETCH_CONFIG_DIR`, `HFETCH_DATA_DIR`, `HFETCH_CACHE_DIR` override `HFETCH_HOME` for their respective role.
- If nothing is set, XDG defaults apply.

**Directory creation:** directories are created on first use with `0700` permissions. This is Linux/macOS first; Windows uses best-effort ACLs with no hard guarantees.

The `--` separator in directory names mirrors the HuggingFace Hub cache convention while remaining filesystem-safe on all platforms.

## 5. HuggingFace Hub API Integration

### 5.1 Authentication

hfetch provides the **canonical token resolution and storage implementation** used across the toolchain. Downstream tools (llm-run, llm-bench) must use `pkg/config` as the default token provider, but may override via explicit flags or `WithToken()` options for CI and testing.

#### Token Resolution Order

Token is resolved from the first source that returns a value (in priority order):

0. **Explicit override** — CLI `--token` flag or `WithToken()` library option. Highest priority; not persisted. Exists for CI, testing, and one-off scripted use.
1. **`HFETCH_TOKEN` environment variable** — for CI/scripts where a flag per-invocation is impractical.
2. **`$HFETCH_CONFIG_DIR/token.json`** — written by `hfetch login`. The canonical persistent store.
3. **HF CLI compatibility path** — best-effort read of the HuggingFace CLI's token location (`~/.cache/huggingface/token` on Linux/macOS). This path has shifted across HF tooling versions and may differ on Windows. Treatment: non-fatal if absent or unreadable; configurable via `hf_compat_token_path` in `config.json` for non-standard environments.
4. **None** — no token configured. Unauthenticated access (public models only).

Token is passed as `Authorization: Bearer <token>` header on all API requests.

#### Login Flow

```
$ hfetch login

  HuggingFace Login
  ─────────────────
  1. Go to https://huggingface.co/settings/tokens
  2. Create a token with "Read" access (sufficient for downloading models)
  3. Paste it below.

  Token: hf_abc...xyz
  Verifying... ✓ Authenticated as "chuck"

  Token saved to ~/.config/hfetch/token.json
  This token is shared with llm-run and llm-bench — no need to log in again.
```

The login command:
- Accepts the token interactively or via `--token <value>` for scripted use.
- Validates the token by calling `pkg/api.WhoAmI()` (`GET /api/whoami`) before persisting.
- Stores the token in `$HFETCH_CONFIG_DIR/token.json` with `0600` permissions (config dir created with `0700` if it doesn't exist).
- Displays the authenticated username. Account tier is shown if the API provides it, but is not a required field.

#### Token Scopes

- **Read** access is sufficient for all hfetch operations (search, download, metadata).
- Gated models additionally require the user to have accepted the model's terms on the HuggingFace website — the token itself doesn't grant access, it proves identity.

#### Additional Auth Commands

```
hfetch logout                   Remove stored token
hfetch whoami                   Show current auth status and token source
```

`hfetch whoami` is especially useful for debugging auth issues across the toolchain:

```
$ hfetch whoami

  Authenticated as: chuck
  Token source:     ~/.config/hfetch/token.json (via hfetch login)
  Token prefix:     hf_abcd...
```

**Redaction rules:** `whoami` (and any other output that references the token) must never display more than the first 8 characters. No suffix is shown. JSON output (`--json`) follows the same redaction — there is no "show full token" flag.

#### Auth Package (`pkg/auth`) — Types and Errors

`pkg/auth` defines the shared types and sentinel errors used across the toolchain. It has **no network or API dependencies** — it's safe to import from any package without pulling in HTTP clients.

```go
package auth

import "errors"

// Sentinel errors for auth failures. Downstream tools (llm-run, llm-bench)
// should check for these to surface consistent, actionable guidance.
var (
    // ErrAuthRequired is returned when an operation requires authentication
    // but no token is configured. User action: run `hfetch login`.
    ErrAuthRequired = errors.New("hfetch: authentication required — run `hfetch login`")

    // ErrAuthInvalid is returned when the configured token is rejected by
    // the HuggingFace API (expired, revoked, malformed).
    // User action: run `hfetch login` to re-authenticate, or `hfetch whoami` to debug.
    ErrAuthInvalid = errors.New("hfetch: token is invalid — run `hfetch login` to re-authenticate")

    // ErrGatedModel is returned when the user is authenticated but has not
    // accepted the model's terms on the HuggingFace website.
    ErrGatedModel = errors.New("hfetch: model requires license acceptance at huggingface.co")
)

// UserInfo holds identity information from HuggingFace.
// Fields beyond Username are optional — the HF API may not return them
// depending on token scope, account type, or API version changes.
type UserInfo struct {
    Username    string  `json:"username"`
    FullName    string  `json:"fullname,omitempty"`
    Email       string  `json:"email,omitempty"`       // May be empty depending on token scope
    AccountType string  `json:"accountType,omitempty"` // "free", "pro", "enterprise" — if provided
}

// TokenResult pairs a resolved token with its source for debugging.
type TokenResult struct {
    Token  string  // The raw token value
    Source string  // "flag", "env", "config", "hf-compat", "none"
}
```

#### Config Package (`pkg/config`) — Token Resolution and Storage

`pkg/config` handles token resolution and persistence. It reads from disk and environment only — **no network calls, no API dependency**. This keeps the import graph clean for downstream consumers.

```go
package config

// ResolveToken returns the HuggingFace API token using the standard
// resolution order (flag → env → config file → HF compat → none).
// The optional override parameter corresponds to an explicit --token flag
// or WithToken() option; pass empty string to skip.
func ResolveToken(override string) auth.TokenResult

// StoreToken persists a token to $HFETCH_CONFIG_DIR/token.json.
// Creates the config directory (0700) and file (0600) if needed.
func StoreToken(token string) error

// ClearToken removes the stored token.
func ClearToken() error

// Dirs returns the resolved XDG directory paths.
func Dirs() DirConfig

type DirConfig struct {
    Config string  // ~/.config/hfetch (or override)
    Data   string  // ~/.local/share/hfetch (or override)
    Cache  string  // ~/.cache/hfetch (or override)
}
```

#### API Package (`pkg/api`) — Token Validation

Token validation lives in `pkg/api` because it requires a network call. The CLI `hfetch login` orchestrates the flow: read token → call `api.WhoAmI()` → call `config.StoreToken()`.

```go
package api

// WhoAmI validates a token against the HuggingFace API and returns
// the authenticated user's information.
// Returns auth.ErrAuthInvalid if the token is rejected.
func (c *Client) WhoAmI(ctx context.Context) (*auth.UserInfo, error)
```

### 5.2 API Endpoints Used

| Endpoint | Purpose |
|---|---|
| `GET /api/models` | Search/list models with filters |
| `GET /api/models/{repo_id}` | Get model metadata |
| `GET /api/models/{repo_id}/tree/{revision}` | List files in a model repo |
| `GET /{repo_id}/resolve/{revision}/{filename}` | Download a specific file |
| `HEAD /{repo_id}/resolve/{revision}/{filename}` | Get file size/hash without downloading |

### 5.3 Rate Limiting

- Respect `X-RateLimit-*` headers from HF API responses.
- Implement exponential backoff with jitter on 429 responses.
- Default to conservative concurrency (2 parallel API requests) to avoid triggering abuse detection.

## 6. Download Engine

### 6.1 Resumable Downloads

Large model files (tens of GB) require robust download handling:

- Use HTTP Range requests to download in chunks (default chunk size: 64 MB).
- Persist download state to `<filename>.gguf.partial` as a JSON sidecar tracking completed byte ranges.
- On resume, validate completed chunks via byte range and continue from the last incomplete chunk.
- On completion, verify full file SHA256 against the HF-provided hash.

**Atomic finalization:** downloads must not produce a "complete but corrupt" file on power loss or crash:

1. Download writes to `<filename>.gguf.partial` (the data) + `<filename>.gguf.state` (the byte-range sidecar).
2. On final chunk completion, verify full SHA256.
3. `fsync` the data file.
4. `rename` from `<filename>.gguf.partial` → `<filename>.gguf` (atomic on POSIX).
5. Remove the `.state` sidecar only after rename succeeds.

The rename is the commit point. If the process dies before step 4, the next run sees the `.partial` file and resumes. If it dies after step 4, the `.gguf` is complete and verified. There is no window where a corrupt file exists under the final name.

### 6.2 Parallel Download Streams

- Default: 4 concurrent download streams per file (configurable).
- For multi-file downloads (e.g., split models), download files sequentially but use parallel streams within each file.
- Bandwidth throttling option: `--max-bandwidth` flag to avoid saturating the network.

### 6.3 Progress Reporting

- Real-time terminal progress showing:
  - Per-file progress bar with percentage, speed (MB/s), and ETA.
  - Overall progress when downloading multiple files.
- Machine-readable progress via `--json` flag for integration with other tools.
- Callback interface in the library for programmatic progress tracking:

```go
type ProgressFunc func(event ProgressEvent)

type ProgressEvent struct {
    File           string
    BytesCompleted int64
    BytesTotal     int64
    Speed          float64  // bytes per second
    Phase          string   // "downloading", "verifying", "complete"
}
```

## 7. GGUF Awareness

### 7.1 Header Parsing

Parse GGUF file headers to extract metadata without downloading the full file:

- Use HTTP Range requests to fetch only the header portion (first ~4 KB typically sufficient for metadata).
- Parse the GGUF magic number, version, tensor count, and metadata key-value pairs.
- Support GGUF v2 and v3 formats.

### 7.2 Quantization Intelligence

When listing files for a model, present them in terms meaningful to a llama.cpp user:

```
$ hfetch files bartowski/Qwen2.5-Coder-32B-Instruct-GGUF

  File                                          Quant     Size      Bits/Weight
  ──────────────────────────────────────────────────────────────────────────────
  Qwen2.5-Coder-32B-Instruct-Q8_0.gguf         Q8_0      34.1 GB   8.00
  Qwen2.5-Coder-32B-Instruct-Q6_K.gguf         Q6_K      26.4 GB   6.57
  Qwen2.5-Coder-32B-Instruct-Q5_K_M.gguf       Q5_K_M    23.1 GB   5.67
  Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf       Q4_K_M    19.9 GB   4.85
  Qwen2.5-Coder-32B-Instruct-IQ4_XS.gguf       IQ4_XS    17.4 GB   4.25
```

### 7.3 Model Fit Estimation

Given known hardware specs (e.g., DGX Spark with 128 GB unified memory), estimate whether a model file will fit:

- Parse the GGUF header for parameter count and quantization type.
- Estimate runtime memory requirement (model weights + KV cache for a given context length).
- Display a simple fit/warning/no-fit indicator alongside file listings.
- This is an estimate, not a guarantee — clearly labeled as such.

## 8. CLI Interface

### 8.1 Bare-Arg Shorthand

When the first argument contains a `/` and doesn't match any subcommand, hfetch treats it as an interactive pull — equivalent to `hfetch pull <model_id>` with no flags:

```
# These are equivalent:
hfetch bartowski/Qwen2.5-Coder-32B-Instruct-GGUF
hfetch pull bartowski/Qwen2.5-Coder-32B-Instruct-GGUF
```

The bare form is the "I'm at my desk, show me what's available" path. It always opens the interactive `huh` picker. Flags (`--quant`, `--streams`, etc.) require the explicit `pull` subcommand — this avoids duplicating pull's flag definitions on the root command, which Cobra doesn't handle cleanly.

**Cobra implementation:** the root command's `RunE` checks `strings.Contains(args[0], "/")` and delegates to the pull handler. Subcommand names never contain `/`, so there's no routing ambiguity.

### 8.2 Commands

```
hfetch <model_id>               Interactive pull shorthand (opens GGUF picker)

hfetch search <query>           Search HuggingFace for models
  --gguf                        Only show repos containing GGUF files
  --sort <field>                Sort by: downloads, updated, trending
  --limit <n>                   Max results (default: 20)
  --token <value>               Override token for this invocation

hfetch info <model_id>          Show detailed model information
  --files                       Include file listing
  --remote                      Force fresh API fetch (skip cache)
  --token <value>               Override token for this invocation

hfetch files <model_id>         List files in a model repo
  --quant <type>                Filter by quantization type
  --min-size <size>             Minimum file size
  --max-size <size>             Maximum file size
  --token <value>               Override token for this invocation

hfetch pull <model_id>          Download a model
  [filename]                    Specific file (default: interactive selection)
  --quant <type>                Auto-select file by quantization type
  --output <dir>                Override download directory
  --streams <n>                 Parallel download streams (default: 4)
  --max-bandwidth <rate>        Bandwidth limit (e.g. "100MB/s")
  --verify                      Re-verify SHA256 after download (default: true)
  --token <value>               Override token for this invocation

hfetch list                     List downloaded models
  --json                        JSON output for scripting
  --path                        Show full file paths

hfetch path <model_id>          Print the local path to a downloaded model file
  [filename]                    Specific file (default: largest GGUF)

hfetch rm <model_id>            Remove a downloaded model
  [filename]                    Remove specific file only

hfetch gc                       Remove partial downloads and orphaned files

hfetch login                    Authenticate with HuggingFace
  --token <value>               Provide token non-interactively

hfetch logout                   Remove stored authentication token

hfetch whoami                   Show current auth status and token source

hfetch config                   Show/edit configuration
  set <key> <value>
  get <key>
```

### 8.4 Interactive Selection

When `hfetch <model_id>` (bare-arg) or `hfetch pull <model_id>` is called without a filename or `--quant`, the interactive `huh` picker opens.

**GGUF-first filtering:** When a repo contains GGUF files, the picker shows only GGUF files by default — non-GGUF files (README, config.json, tokenizer files, etc.) are noise in this workflow. Pass `--all-files` to `pull` to show everything.

If the repo contains no GGUF files, all files are shown with a note that no GGUF files were found.

```
$ hfetch bartowski/Qwen2.5-Coder-32B-Instruct-GGUF

  bartowski/Qwen2.5-Coder-32B-Instruct-GGUF

  Select a file to download:

  > Q4_K_M    19.9 GB   ✓ Fits    Best balance of quality/size
    Q5_K_M    23.1 GB   ✓ Fits    Higher quality
    Q6_K      26.4 GB   ✓ Fits    Near-lossless
    Q8_0      34.1 GB   ✓ Fits    Highest quality
    IQ4_XS    17.4 GB   ✓ Fits    Smallest, lower quality

  ↑/↓ navigate • enter select • q quit
```

The picker is rendered by `charmbracelet/huh` via `huh.NewSelect`. It supports arrow-key navigation, type-to-filter, and degrades to plain numbered input if the terminal doesn't support alternate screen mode.

### 8.3 Environment Variables

| Variable | Purpose | Default |
|---|---|---|
| `HFETCH_HOME` | Convenience override — remaps config/data/cache subdirs | (none, uses XDG) |
| `HFETCH_CONFIG_DIR` | Config directory (token, settings) | `~/.config/hfetch` |
| `HFETCH_DATA_DIR` | Data directory (models, manifest) | `~/.local/share/hfetch` |
| `HFETCH_CACHE_DIR` | Cache directory (API metadata cache) | `~/.cache/hfetch` |
| `HFETCH_TOKEN` | HuggingFace API token (overrides stored token) | (none) |
| `HFETCH_STREAMS` | Default parallel download streams | `4` |
| `HFETCH_VRAM` | Available memory in GB for fit estimation | auto-detect if possible |

## 9. Go Library API

The library is the primary interface. The CLI is a consumer of it.

```go
package hfetch

// Client is the main entry point for the hfetch library
type Client struct { ... }

// NewClient creates a new HuggingFace client with the given options
func NewClient(opts ...Option) (*Client, error)

// Option configures the client
type Option func(*clientConfig)

// WithToken provides an explicit token override (priority 0 in resolution order).
// If not set, the client resolves the token via pkg/config.ResolveToken("").
// This exists for CI, testing, and programmatic use — it does NOT bypass
// the canonical resolution; it takes precedence over it.
func WithToken(token string) Option

func WithCacheDir(dir string) Option
func WithHTTPClient(client *http.Client) Option

// Search finds models on HuggingFace Hub
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) ([]Model, error)

// GetModel retrieves metadata for a specific model
func (c *Client) GetModel(ctx context.Context, modelID string) (*Model, error)

// ListFiles lists files in a model repository
func (c *Client) ListFiles(ctx context.Context, modelID string) ([]ModelFile, error)

// Pull downloads a model file
func (c *Client) Pull(ctx context.Context, modelID, filename string, opts PullOptions) (*LocalFile, error)

// PullOptions configures a download
type PullOptions struct {
    OutputDir    string
    Streams      int
    MaxBandwidth int64          // bytes per second, 0 = unlimited
    OnProgress   ProgressFunc
}

// Registry provides access to locally downloaded models
func (c *Client) Registry() *Registry

// Registry tracks downloaded models
type Registry struct { ... }
func (r *Registry) List() ([]LocalModel, error)
func (r *Registry) Get(modelID string) (*LocalModel, error)
func (r *Registry) Path(modelID, filename string) (string, error)
func (r *Registry) Remove(modelID string, filenames ...string) error
func (r *Registry) GC() (freed int64, err error)
```

## 10. Integration Points

### 10.1 With llama.cpp Wrapper (llm-run)

The wrapper imports `hfetch/pkg/registry` to resolve model references:

```go
// The wrapper can accept model references in multiple forms:
// 1. Local path:     /path/to/model.gguf
// 2. Registry name:  bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M
// 3. HuggingFace ID: hf://bartowski/Qwen2.5-Coder-32B-Instruct-GGUF
//
// Forms 2 and 3 resolve through the hfetch registry.
// Form 3 triggers an auto-download if the model isn't cached locally.
```

### 10.2 With Benchmark Suite (llm-bench)

The benchmark suite uses the library to:
- Resolve model identifiers to local paths.
- Pull models that are referenced in benchmark configs but not yet downloaded.
- Read GGUF metadata (parameter count, quantization) to include in benchmark reports.

## 11. Error Handling

### 11.1 Auth Errors

All auth-related failures use the typed errors defined in `pkg/auth`. This ensures consistent, actionable guidance whether the error surfaces in hfetch, llm-run, or llm-bench:

| Error | Trigger | User Guidance |
|---|---|---|
| `auth.ErrAuthRequired` | Operation needs auth but no token resolved | "Run `hfetch login` to authenticate" |
| `auth.ErrAuthInvalid` | Token rejected by HF API (401) | "Run `hfetch login` to re-authenticate, or `hfetch whoami` to check current token" |
| `auth.ErrGatedModel` | Authenticated but model terms not accepted (403) | "Accept the model license at huggingface.co/<model> then retry" |

Downstream tools (llm-run auto-pull, llm-bench pre-flight) must check for these errors by type and surface the guidance messages directly — never invent their own auth instructions.

**Important:** not all HTTP failures are auth failures. The error mapping is:

| HTTP Status | Error | Behavior |
|---|---|---|
| 401 | `auth.ErrAuthInvalid` | Surface auth guidance, do not retry |
| 403 | `auth.ErrGatedModel` | Surface license guidance, do not retry |
| 429 | (rate limit) | Obey `Retry-After` header, exponential backoff with jitter |
| 5xx | (server error) | Retry with exponential backoff, bounded (max 3 retries) |

### 11.2 Network and Download Errors

- Network errors during download: retry with exponential backoff (max 5 retries per chunk).
- Disk full during download: detect early via free space check before starting, fail gracefully with cleanup.
- Corrupt downloads: SHA256 mismatch triggers automatic re-download of affected chunks.

### 11.3 Token Storage Errors

- Config directory not writable: fail with clear message showing the path and expected permissions (`0700`).
- Token file not writable: fail with clear message showing the path and expected permissions (`0600`).

## 12. Testing Strategy

- **Unit tests:** API response parsing, GGUF header parsing, manifest operations, path resolution.
- **Integration tests:** Against a mock HTTP server that mimics HF API responses.
- **Download tests:** Use a local HTTP server serving known files to test resumption, parallelism, and verification.
- **No live HF API tests in CI.** Live tests gated behind `HFETCH_LIVE_TEST=1` for manual/periodic validation.

## 13. Future Considerations

- **Model conversion:** If GGUF conversion from safetensors becomes feasible in pure Go, this would be the natural home for it.
- **Hub caching protocol:** HuggingFace is evolving their caching/mirroring protocols. Monitor and adapt.
- **Model cards:** Parsing and displaying model card content (currently markdown) for quick reference.
- **Collections/tags:** Supporting HuggingFace collections for curated model groups.
- **Multi-token profiles:** `token.json` schema reserves space for named token profiles (e.g., personal vs. org tokens). Current schema: `{"default": "hf_..."}`. Future: `{"default": "hf_...", "profiles": {"work": "hf_...", "personal": "hf_..."}}`. Not implemented now — schema is forward-compatible.
