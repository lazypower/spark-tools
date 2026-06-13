# llm-tidy — Model Inventory Management

**Status:** Draft
**Created:** 2026-03-20
**Component:** `llm-tidy`
**Language:** Go (pure — zero cgo)
**Depends on:** `hfetch` (GGUF registry, model metadata)

---

## 1. Problem Statement

Local LLM work generates model cruft. The problem manifests differently on different machines:

On a dedicated inference box (e.g., a DGX Spark running llm-run with GGUFs on disk), model selection is intentional and the inventory stays small. But on an experimentation machine running Ollama, every `ollama pull` and `ollama create` leaves a persistent model registration. After a few weeks of trying quantization variants, evaluating new releases, and one-off experiments, the model list grows from a curated toolbox into a junk drawer. Disk fills up with 30–50 models totaling hundreds of gigabytes, most of which haven't been touched since the day they were pulled.

There's no built-in way to distinguish "models I depend on" from "models I tried once." There's no `ollama gc`. The only remediation is scrolling through `ollama list`, trying to remember which models matter, and manually running `ollama rm` one at a time.

`llm-tidy` solves this with declarative model inventory management. Define the models that should exist on a machine, and let the tool handle the diff: flag what's untracked, prune what doesn't belong, pull what's missing. It works across both backends — Ollama's model store and hfetch's GGUF registry — giving a unified view of local model inventory.

## 2. Goals

1. **Declarative desired state.** A YAML manifest defines which models should be present on this machine, for each backend.
2. **Unified inventory.** Single view across Ollama models and hfetch GGUF models. Know what's on disk regardless of which backend manages it.
3. **Safe pruning.** Interactive by default. Show what will be removed and how much disk will be reclaimed before doing anything destructive.
4. **Promote/demote workflow.** Experimenting with a model should be frictionless. Keeping it long-term should be an explicit decision. Moving between those states should be one command.
5. **Sync.** Pull anything declared in the manifest that isn't present locally. The manifest is the source of truth.
6. **Library-first design.** The CLI is a thin shell over `pkg/llmtidy`. Other tools or scripts can import the reconciliation logic.

## 3. Non-Goals

- Managing model runtime parameters (context window, temperature, system prompts). Those are Ollama Modelfile concerns or llm-run profile concerns — different tools, different lifecycle.
- Replacing `ollama` CLI for day-to-day model operations (pull, run, show).
- Managing models on remote machines. This is a local tool. Fleet management is a different problem.
- Automatic model updates or version tracking. The manifest pins specific model references; upgrading is a manual edit.
- Building or converting models. Use `hfetch ollama-import` for GGUF-to-Ollama, `hfetch pull` for downloads.

## 4. Architecture

### 4.1 Package Structure

```
llm-tidy/
├── cmd/llm-tidy/              # CLI entrypoint
│   ├── main.go
│   ├── status.go              # status subcommand
│   ├── prune.go               # prune subcommand
│   ├── sync.go                # sync subcommand
│   ├── promote.go             # promote subcommand
│   ├── demote.go              # demote subcommand
│   └── init.go                # init subcommand
├── pkg/llmtidy/
│   ├── manifest/              # Manifest parsing, validation, modification
│   │   ├── manifest.go        # Types, load, save
│   │   ├── resolve.go         # Manifest file discovery (XDG, flags)
│   │   └── validate.go        # Schema validation
│   ├── inventory/             # Backend-agnostic model inventory
│   │   ├── inventory.go       # Unified inventory types
│   │   ├── ollama.go          # Ollama backend: list, delete, pull
│   │   ├── gguf.go            # GGUF/hfetch backend: list, delete
│   │   └── types.go           # InstalledModel, Backend enum
│   ├── reconcile/             # Diff and apply logic
│   │   ├── diff.go            # Compare manifest vs. inventory
│   │   ├── plan.go            # Build a reconciliation plan
│   │   └── apply.go           # Execute the plan (prune, sync)
│   └── ollama/                # Ollama API client (minimal)
│       ├── client.go          # HTTP client for Ollama REST API
│       └── types.go           # API response types
```

### 4.2 Terminal UI Stack

Same charmbracelet stack as the rest of the toolchain:

- **`charmbracelet/huh`** — confirmation prompts for prune operations, interactive model selection.
- **`charmbracelet/lipgloss`** — styled tables for status output, consistent with hfetch/llm-run visual language.

### 4.3 Core Types

```go
// Manifest represents the desired model state for a machine.
type Manifest struct {
    Version int                `yaml:"version"`  // Schema version, currently 1
    Ollama  []OllamaModelSpec `yaml:"ollama"`
    GGUF    []GGUFModelSpec   `yaml:"gguf"`
}

// OllamaModelSpec declares a model that should exist in Ollama.
type OllamaModelSpec struct {
    Name string `yaml:"name"` // e.g. "qwen2.5-coder:32b", "llama3.3:70b"
}

// GGUFModelSpec declares a model that should exist in the hfetch registry.
type GGUFModelSpec struct {
    Repo  string `yaml:"repo"`            // e.g. "unsloth/Qwen3.5-122B-A10B-GGUF"
    Quant string `yaml:"quant,omitempty"` // e.g. "Q4_K_M" — if omitted, any quant matches
}

// InstalledModel represents a model found on disk, regardless of backend.
type InstalledModel struct {
    Name     string    // Display name (ollama name or hfetch repo:quant)
    Backend  Backend   // Ollama or GGUF
    Size     int64     // Size in bytes
    Modified time.Time // Last modified timestamp
}

type Backend int

const (
    BackendOllama Backend = iota
    BackendGGUF
)

// DiffResult categorizes all installed and declared models.
type DiffResult struct {
    Blessed   []InstalledModel // In manifest AND installed
    Untracked []InstalledModel // Installed but NOT in manifest
    Missing   []ModelSpec      // In manifest but NOT installed
}

// ModelSpec is a union type for either backend's spec.
type ModelSpec struct {
    Backend Backend
    Ollama  *OllamaModelSpec
    GGUF    *GGUFModelSpec
}
```

### 4.4 Manifest File

#### Location

Manifest is resolved in priority order:

1. **`--manifest` flag** — explicit path, highest priority.
2. **`LLM_TIDY_MANIFEST` env var** — for scripted use.
3. **`$LLM_TIDY_CONFIG_DIR/manifest.yaml`** — canonical persistent location.
4. **XDG default** — `~/.config/llm-tidy/manifest.yaml`.

#### Format

```yaml
# ~/.config/llm-tidy/manifest.yaml
version: 1

ollama:
  - name: gpt-oss-120b:latest
  - name: qwen3.5-122b-a10b-iq3s:latest
  - name: qwen3.5-35b-a3b-q8:latest
  - name: qwen2.5-coder:32b
  - name: llama3.3:70b
  - name: nomic-embed-text:latest

gguf:
  - repo: unsloth/Qwen3.5-122B-A10B-GGUF
    quant: Q4_K_M
  - repo: mradermacher/Venus-120b-v1.0-i1-GGUF
```

#### Matching Rules

**Ollama models** are matched by name. The name follows Ollama's `model:tag` convention. If no tag is specified in the manifest, `:latest` is implied — matching Ollama's own default behavior.

**GGUF models** are matched by repo ID and optional quant. If quant is omitted, any file from that repo matches. This handles the case where a user wants to keep "whatever I downloaded from this repo" without specifying the exact quantization.

## 5. Ollama API Integration

### 5.1 API Endpoints

llm-tidy uses Ollama's REST API directly rather than shelling out to the `ollama` CLI. This avoids parsing human-formatted output and works whether or not the `ollama` binary is on PATH (the Ollama server may be running as a service).

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/tags` | GET | List all locally available models |
| `/api/show` | POST | Get model details (size, parameters, template) |
| `/api/delete` | DELETE | Remove a model |
| `/api/pull` | POST | Pull a model (for sync) |

### 5.2 Connection

Default: `http://localhost:11434` (Ollama's default).

Override via `OLLAMA_HOST` environment variable — this is the same env var Ollama itself uses, so llm-tidy automatically follows whatever the user's Ollama is configured to.

### 5.3 Availability

Ollama backend is opportunistic. If the Ollama server is unreachable:
- `status` shows the GGUF inventory only, with a note that Ollama is unavailable.
- `prune` and `sync` for Ollama models are skipped with a warning.
- This is not an error — a machine may only have one backend.

## 6. hfetch/GGUF Integration

llm-tidy imports `pkg/hfetch/registry` to enumerate local GGUF models. This reuses the existing manifest and storage layout — no new scanning logic needed.

For removal, it calls `registry.Remove()`. For sync (downloading missing models), it uses the `hfetch` library's `Pull()` API with interactive file selection when a quant isn't specified.

## 7. CLI Interface

### 7.1 Commands

```
llm-tidy status                Show inventory vs. manifest
  --backend <ollama|gguf>      Filter to one backend
  --json                       JSON output for scripting

llm-tidy prune                 Remove models not in manifest
  --backend <ollama|gguf>      Filter to one backend
  --dry-run                    Show what would be removed without removing
  --yes, -y                    Skip confirmation prompt
  --older-than <duration>      Only prune untracked models older than this (e.g. "7d", "30d")

llm-tidy sync                  Pull models in manifest that are missing locally
  --backend <ollama|gguf>      Filter to one backend
  --dry-run                    Show what would be pulled without pulling

llm-tidy promote <model>       Add a model to the manifest
  --backend <ollama|gguf>      Required if ambiguous

llm-tidy demote <model>        Remove a model from the manifest

llm-tidy init                  Create a manifest from current inventory
  --backend <ollama|gguf>      Filter to one backend
  --output <path>              Write to a specific path (default: XDG config)
```

### 7.2 Status Output

```
$ llm-tidy status

  Ollama Models (6 blessed, 15 untracked)

  BLESSED
  ✓ gpt-oss-120b:latest                     63 GB
  ✓ qwen3.5-122b-a10b-iq3s:latest           46 GB
  ✓ qwen3.5-35b-a3b-q8:latest               36 GB
  ✓ qwen2.5-coder:32b                       19 GB
  ✓ llama3.3:70b                             42 GB
  ✓ nomic-embed-text:latest                  274 MB

  UNTRACKED
  ● hunyuan-a13b-q8:latest                   39 GB    10 days ago
  ● qwen3-30b-a3b-bf16:latest                49 GB    10 days ago
  ● hunyuan-a13b-q4km:latest                 49 GB    10 days ago
  ● magnum-diamond-24b:latest                14 GB    11 days ago
  ● cydonia-24b:latest                       14 GB    11 days ago
  ...

  Untracked total: 412 GB across 15 models

  GGUF Models (2 blessed, 0 untracked)

  BLESSED
  ✓ unsloth/Qwen3.5-122B-A10B-GGUF Q4_K_M   46.5 GB
  ✓ mradermacher/Venus-120b-v1.0-i1-GGUF     45.8 GB

  Use 'llm-tidy prune' to remove untracked models.
  Use 'llm-tidy promote <model>' to add a model to your manifest.
```

### 7.3 Prune Output

```
$ llm-tidy prune

  The following untracked models will be removed:

  Ollama:
    hunyuan-a13b-q8:latest                   39 GB
    qwen3-30b-a3b-bf16:latest                49 GB
    hunyuan-a13b-q4km:latest                 49 GB
    magnum-diamond-24b:latest                14 GB
    cydonia-24b:latest                       14 GB
    ... (10 more)

  Total to reclaim: 412 GB

  Remove these models? [y/N]
```

### 7.4 Init Flow

`llm-tidy init` bootstraps a manifest from the current state — every installed model becomes blessed. This is the "declare bankruptcy and start fresh" path: run init, then edit the manifest to remove what you don't want, then prune.

```
$ llm-tidy init

  Scanned 21 Ollama models and 2 GGUF models.
  Wrote manifest to ~/.config/llm-tidy/manifest.yaml

  Edit the manifest to remove models you don't want to keep,
  then run 'llm-tidy prune' to clean up.
```

### 7.5 Environment Variables

| Variable | Purpose | Default |
|---|---|---|
| `LLM_TIDY_CONFIG_DIR` | Config directory (manifest location) | `~/.config/llm-tidy` |
| `LLM_TIDY_MANIFEST` | Explicit manifest path | (none, uses config dir) |
| `OLLAMA_HOST` | Ollama server address | `http://localhost:11434` |

## 8. Go Library API

```go
package llmtidy

// Tidy is the main entry point for the llm-tidy library.
type Tidy struct { ... }

// New creates a new Tidy instance with the given options.
func New(opts ...Option) (*Tidy, error)

type Option func(*config)

func WithManifestPath(path string) Option
func WithOllamaHost(host string) Option

// LoadManifest reads and validates the manifest file.
func (t *Tidy) LoadManifest() (*Manifest, error)

// Inventory returns all installed models across all available backends.
func (t *Tidy) Inventory(ctx context.Context) ([]InstalledModel, error)

// Diff compares the manifest against installed models.
func (t *Tidy) Diff(ctx context.Context) (*DiffResult, error)

// Prune removes untracked models. Returns the list of removed models
// and total bytes reclaimed. The filter function, if non-nil, is called
// for each candidate — return true to remove, false to skip.
func (t *Tidy) Prune(ctx context.Context, filter func(InstalledModel) bool) ([]InstalledModel, int64, error)

// Sync pulls models that are in the manifest but not installed.
func (t *Tidy) Sync(ctx context.Context) ([]InstalledModel, error)

// Promote adds a model to the manifest and saves.
func (t *Tidy) Promote(ctx context.Context, model string, backend Backend) error

// Demote removes a model from the manifest and saves.
func (t *Tidy) Demote(ctx context.Context, model string) error

// Init creates a manifest from the current inventory.
func (t *Tidy) Init(ctx context.Context) (*Manifest, error)
```

## 9. Error Handling

| Condition | Error | Guidance |
|-----------|-------|----------|
| No manifest found | `no manifest found` | `Run: llm-tidy init` |
| Invalid manifest YAML | `manifest parse error at line N` | Show the parse error with context |
| Ollama unreachable | `ollama not available at <host>` | `Check OLLAMA_HOST or start the Ollama service` |
| Ollama delete fails | Pass through Ollama's error | — |
| hfetch registry unreadable | `hfetch registry not found` | `Run: hfetch list` to initialize, or check `HFETCH_DATA_DIR` |
| Model not found for promote | `model "<name>" not found in any backend` | `Check: ollama list` or `hfetch list` |
| Model not found for demote | `model "<name>" not in manifest` | Show closest matches |
| Manifest not writable | `cannot write manifest: <path>` | Show path and permissions |

## 10. Reconciliation Logic

### 10.1 Diff Algorithm

```
Diff(manifest, inventory):
    blessed   = []
    untracked = []
    missing   = []

    for each model in inventory:
        if matches any entry in manifest:
            blessed.append(model)
        else:
            untracked.append(model)

    for each spec in manifest:
        if no installed model matches:
            missing.append(spec)

    return DiffResult{blessed, untracked, missing}
```

### 10.2 Matching Semantics

**Ollama:** Exact name match. `qwen2.5-coder:32b` matches `qwen2.5-coder:32b`. Tag normalization: missing tag in manifest implies `:latest`, matching Ollama's convention.

**GGUF:** Match by repo ID (case-insensitive). If quant is specified in the manifest, only that quant matches. If quant is omitted, any file from the repo matches. This handles repos where you've downloaded multiple quants — either bless them all or bless a specific one.

### 10.3 Prune Safety

Prune is destructive and follows safety-first principles:

1. **Interactive by default.** Shows the full removal list with sizes before prompting for confirmation.
2. **Dry-run available.** `--dry-run` shows the plan without executing.
3. **Age filter.** `--older-than 7d` limits pruning to models that haven't been modified recently — protects experiments still in progress.
4. **No cascading deletes.** Removing an Ollama model doesn't touch the underlying GGUF if it was imported via `hfetch ollama-import`. The two storage systems are independent.
5. **Never touches blessed models.** A model in the manifest is sacred, even if it hasn't been used in months.

## 11. Testing Strategy

- **Unit tests:** Manifest parsing/validation, diff algorithm, matching semantics, promote/demote manifest mutations.
- **Integration tests:** Mock HTTP server mimicking Ollama's `/api/tags`, `/api/delete`, `/api/pull` responses. Mock hfetch registry for GGUF inventory.
- **No live Ollama tests in CI.** Live tests gated behind `LLM_TIDY_LIVE_TEST=1`.
- **Fixture manifests:** YAML test fixtures covering edge cases (empty manifest, missing tags, duplicate entries, unknown backends).

## 12. Future Considerations

- **Machine labels.** A single manifest with per-machine sections (`machines.strix.ollama: [...]`). Useful if the manifest is checked into a dotfiles repo. Not in v1 — keep it simple with one manifest per machine.
- **Last-used tracking.** Ollama doesn't expose "last used" timestamps today. If it ever does, `--older-than` could filter on actual usage rather than modification time, making auto-prune much smarter.
- **Scheduled prune.** A cron/systemd timer that runs `llm-tidy prune --yes --older-than 30d` to keep the junk drawer from growing. Trivial to set up manually; not worth building into the tool.
- **Ollama Modelfile management.** If users want to manage custom Modelfiles (temperature, system prompt, template), that could be a separate manifest section. Deliberately excluded from v1 — the problem being solved is inventory, not configuration.
- **Disk budget.** `max_disk: 500GB` in the manifest, with prune auto-selecting the oldest/largest untracked models to stay under budget. Interesting but adds complexity to the prune algorithm.
