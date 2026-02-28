# llm-run — llama.cpp Wrapper for Humans

**Status:** Draft
**Created:** 2026-02-27
**Component:** `llm-run`
**Language:** Go
**Depends on:** `hfetch` (model resolution and GGUF metadata)

---

## 1. Problem Statement

llama.cpp is the fastest inference engine for running quantized LLMs on local hardware. On a DGX Spark GB10 with 128 GB unified memory, it delivers ~50 tokens/second on a 120B model — genuine production-grade local inference.

The problem: llama.cpp's CLI is powerful but hostile. `llama-server` and `llama-cli` expose dozens of flags with arcane names, no sensible defaults for common workflows, and no memory of what worked last time. Every session begins with reconstructing a command line from scattered notes. The gap between "I have a model file" and "I'm having a conversation" is unnecessarily wide.

`llm-run` closes that gap. It wraps llama.cpp's executables with ergonomic defaults, persistent configuration, model resolution via `hfetch`, and a command interface designed for the common cases while preserving escape hatches to the full power underneath.

## 2. Goals

1. **One-command inference.** `llm-run chat qwen-32b` should Just Work — resolve the model, pick sane parameters, launch `llama-server` or `llama-cli`, and connect.
2. **Smart defaults derived from hardware and model.** Auto-detect available memory, GPU layers, context length, and batch size. Don't make the user calculate.
3. **Profile system.** Save named configurations (model + parameters) for instant recall. `llm-run chat --profile coding` brings up the exact setup from last time.
4. **Native hfetch integration.** Model references resolve through the `hfetch` registry. Missing models can be auto-pulled.
5. **Composable.** Works as both CLI and Go library. The benchmark suite imports it to launch inference processes programmatically.

## 3. Non-Goals

- Replacing llama.cpp. We wrap it, not fork it.
- Building our own inference engine.
- Supporting inference backends other than llama.cpp (for now).
- Providing a web UI. The focus is CLI and programmatic access. llama.cpp's built-in web UI is available when running in server mode.

## 4. Architecture

### 4.1 Package Structure

```
llm-run/
├── cmd/llm-run/              # CLI entrypoint
├── pkg/
│   ├── engine/               # llama.cpp process management
│   │   ├── detect.go         # Find llama.cpp binaries on the system
│   │   ├── launch.go         # Build command lines, launch processes
│   │   ├── monitor.go        # Health checking, process lifecycle
│   │   └── config.go         # Translate high-level config to llama.cpp flags
│   ├── resolver/             # Model resolution
│   │   ├── resolve.go        # Parse model references, resolve to file paths
│   │   └── aliases.go        # Short name aliases for frequently used models
│   ├── profiles/             # Saved configurations
│   │   ├── profile.go        # Profile CRUD
│   │   └── defaults.go       # Built-in default profiles
│   ├── hardware/             # Hardware detection
│   │   ├── detect.go         # CPU, memory, GPU detection
│   │   ├── dgx.go            # DGX Spark specific detection and tuning
│   │   └── recommend.go      # Parameter recommendations based on hardware
│   ├── api/                  # Client for llama-server's OpenAI-compatible endpoints
│   │   ├── client.go         # HTTP client targeting llama-server's /v1/* routes
│   │   └── types.go          # OpenAI-compatible request/response types
│   └── config/               # Global configuration
│       └── config.go
└── internal/
    └── tui/                  # Terminal UI components (charmbracelet/bubbletea)
        ├── chat.go           # Interactive chat interface (bubbletea Model)
        ├── status.go         # Server status display
        └── style.go          # Shared lipgloss styles
```

### 4.2 Terminal UI Stack

All interactive terminal components use the [Charm](https://charm.sh) library family:

- **[`charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea)** — the full Model-View-Update TUI framework. Powers the interactive chat interface, which needs streaming token display, slash command handling, status bars, and terminal resize handling. This is heavier than huh but necessary for the real-time chat loop.
- **[`charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss)** — styling for the chat chrome, server status boxes, stats tables. Shared across the toolchain for visual consistency with hfetch and llm-bench.
- **[`charmbracelet/huh`](https://github.com/charmbracelet/huh)** — used only for simpler interactive prompts (e.g., model selection during auto-pull, profile selection). Not used for the chat interface itself.

Same Charm stack as hfetch and llm-bench. Zero cgo.

### 4.3 Core Types

```go
// RunConfig represents a complete inference configuration
type RunConfig struct {
    // Model
    ModelRef       string        // hfetch model reference or local path
    ModelPath      string        // Resolved absolute path to .gguf file

    // Hardware allocation
    GPULayers      int           // Number of layers to offload to GPU (-1 = all)
    Threads        int           // CPU threads for inference
    MainGPU        int           // Primary GPU index

    // Memory and context
    ContextSize    int           // Context window size in tokens
    BatchSize      int           // Batch size for prompt processing
    UBatchSize     int           // Micro-batch size

    // Generation
    Temperature    float64
    TopP           float64
    TopK           int
    RepeatPenalty  float64
    Seed           int           // -1 = random

    // Server mode
    ServerMode     bool
    Host           string
    Port           int

    // Advanced
    FlashAttention bool          // Enable flash attention
    MMap           bool          // Memory-map the model file
    MLock          bool          // Lock model in memory
    NumaStrategy   NumaStrategy  // NUMA strategy (typed enum, validated)
    ExtraArgs      []string      // Pass-through args to llama.cpp
}

// NumaStrategy controls NUMA memory allocation policy.
type NumaStrategy int

const (
    NumaDisabled   NumaStrategy = iota  // No NUMA awareness (default)
    NumaDistribute                       // Distribute across NUMA nodes
    NumaIsolate                          // Isolate to a single NUMA node
)

func (n NumaStrategy) String() string  // Returns "disabled", "distribute", "isolate"
func ParseNumaStrategy(s string) (NumaStrategy, error)  // Validates input

// Profile is a named, saved RunConfig
type Profile struct {
    Name        string
    Description string
    Config      RunConfig
    CreatedAt   time.Time
    UpdatedAt   time.Time
    LastUsedAt  time.Time
}

// HardwareInfo describes the detected hardware
type HardwareInfo struct {
    CPUName       string
    CPUCores      int
    TotalMemoryGB float64
    FreeMemoryGB  float64
    GPUs          []GPUInfo
    IsDGXSpark    bool
    NUMANodes     int
}

// GPUInfo describes a detected GPU
type GPUInfo struct {
    Index      int
    Name       string
    MemoryGB   float64
    Compute    string   // e.g. "sm_100" for GB10
}
```

## 5. llama.cpp Binary Management

### 5.1 Detection

`llm-run` finds llama.cpp binaries through (in priority order):

1. `LLM_RUN_LLAMA_DIR` environment variable pointing to the llama.cpp build directory.
2. Binaries on `$PATH` (`llama-server`, `llama-cli`, `llama-bench`).
3. Common install locations: `/usr/local/bin/`, `/opt/llama.cpp/`.

No sibling-directory conventions or assumptions about hfetch's layout. `LLM_RUN_LLAMA_DIR` is the explicit override; `$PATH` is the standard mechanism; common install paths are a fallback for out-of-the-box setups.

### 5.2 Version and Capability Detection

On first use (and cached until the binary changes), `llm-run` probes the llama.cpp installation to build a capability set:

```go
// Capabilities represents what the detected llama.cpp build supports.
type Capabilities struct {
    Version        string   // Build version/commit hash
    Backend        string   // "cuda", "metal", "vulkan", "cpu"
    CUDACompute    string   // e.g. "sm_100" (empty if not CUDA)
    FlashAttention bool
    NUMA           bool
    MMap           bool
    MLock          bool
    ServerMode     bool     // llama-server binary present
    BenchMode      bool     // llama-bench binary present
}

// Detect populates capabilities by parsing:
//   - `llama-server --version` for version/commit
//   - `llama-server --help` for supported flags
//   - binary presence checks for llama-server, llama-cli, llama-bench
//
// If --help parsing fails (output format changed, unexpected version),
// fall back to conservative defaults: all optional capabilities false,
// backend "cpu". Log a warning so the user knows optimizations are disabled.
```

Warn if the detected version is older than a known-good minimum.

### 5.3 Flag Translation and Capability Gating

`engine/config.go` translates a `RunConfig` into a llama.cpp command line. When the config requests a feature the build doesn't support, behavior depends on the flag class:

**Graceful degradation (warn + disable):**
- `FlashAttention: true` but build doesn't support `--flash-attn` → log warning, omit flag, continue.
- `NumaStrategy: "distribute"` but build doesn't support `--numa` → log warning, omit flag, continue.
- `MLock: true` but build doesn't support `--mlock` → log warning, omit flag, continue.

These are performance optimizations — absence degrades speed, not correctness.

**Hard error (refuse to launch):**
- `GPULayers: -1` but build is CPU-only → error: "llama.cpp was built without GPU support. Rebuild with CUDA or set gpu-layers to 0."
- `ServerMode: true` but `llama-server` binary not found → error: "llama-server not found. Ensure llama.cpp was built with server support."

These would produce silent failures or crashes if ignored.

**Always pass-through:**
- `ExtraArgs` are appended to the command line verbatim, no validation. This is the escape hatch — if llama.cpp rejects them, the user sees llama.cpp's own error output.

```go
// BuildCommand translates a RunConfig into a command line, gated by capabilities.
// Returns the command and a list of warnings for degraded features.
func BuildCommand(cfg RunConfig, caps Capabilities) (cmd []string, warnings []string, err error)
```

## 6. Model Resolution

### 6.1 Disambiguation Rule

Profiles and model references occupy separate namespaces. There is no implicit "check profiles first" behavior — that creates silent collisions when a profile name matches an alias or model ref.

**The rule:** `--profile` is the only way to load a profile. The positional `<model>` argument is always a model reference, never a profile name.

```
# Profile: explicit --profile flag, always.
llm-run chat --profile coding
llm-run chat --profile coding qwen-32b    # Profile settings + model override

# Model ref: positional argument, always.
llm-run chat qwen-32b                     # Alias
llm-run chat /path/to/model.gguf          # Local file
llm-run chat bartowski/Qwen2.5-...:Q4_K_M # hfetch registry ref
```

When `--profile` is provided with a model argument, the profile's settings apply (temperature, context, system prompt, etc.) but the model argument overrides the profile's `modelRef`. This lets you reuse a profile's tuning with a different model.

When `--profile` is provided without a model argument, the profile's `modelRef` is used.

### 6.2 Reference Formats

The positional `<model>` argument is resolved in this order:

```
# 1. Local file path (starts with / or ./ or ~/)
llm-run chat /path/to/model.gguf

# 2. Alias (user-defined short names, no / in name)
llm-run chat qwen-32b

# 3. hfetch registry reference (org/model:quantization)
llm-run chat bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M

# 4. HuggingFace URI (explicit prefix, triggers download if missing)
llm-run chat hf://bartowski/Qwen2.5-Coder-32B-Instruct-GGUF
```

**Detection heuristic** (evaluated in this order — first match wins):

1. Starts with `hf://` → **HuggingFace URI** (auto-pull if missing). Short-circuits first to avoid misclassifying `hf://org/model` as a registry ref due to the `/`.
2. Starts with `/`, `./`, `~/`, or ends with `.gguf` → **local file path**
3. No `/` in the string → **alias lookup** (then error if not found)
4. Contains `/` → **hfetch registry ref** (org/model format)

### 6.3 Structured Resolution Result

`ResolveModel` returns a structured result with full provenance, not just a path. This is essential for llm-bench reproducibility, `llm-run models` display, and debugging "why did it choose this file?"

```go
// ResolvedModel captures how a model reference was resolved.
type ResolvedModel struct {
    Path          string              // Absolute path to the .gguf file on disk
    Source        ResolveSource       // How it was resolved
    RequestedRef  string              // Raw input as the user typed it
    NormalizedRef string              // Canonical form (derived from RequestedRef)
    Quant         string              // Quantization type (e.g. "Q4_K_M")
    RegistryID    string              // hfetch model ID, if resolved via registry
    GGUFMeta      *hfetch.GGUFMetadata // Parsed GGUF header metadata
    WasPulled     bool                // True if hfetch downloaded it during resolution
}

type ResolveSource int

const (
    ResolveSourceLocalPath  ResolveSource = iota  // Direct file path
    ResolveSourceAlias                             // Resolved via alias
    ResolveSourceRegistry                          // Resolved via hfetch registry
    ResolveSourceHFPull                            // Downloaded from HuggingFace
)
```

### 6.4 Aliases

Users define aliases for frequently used models:

```
$ llm-run alias set qwen-32b bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M
$ llm-run alias set deepseek-r1 bartowski/DeepSeek-R1-0528-GGUF:Q4_K_M
$ llm-run alias list
```

Aliases are stored in `$LLM_RUN_CONFIG_DIR/aliases.json`.

### 6.5 Auto-Pull

When a model reference resolves to an `hfetch` registry entry that doesn't exist locally:

**Interactive mode** (default when TTY is attached):
- If the quantization is specified (`:Q4_K_M`), prompt to confirm download of that specific file.
- If no quantization specified, present the `hfetch` interactive picker via `huh`.

**Non-interactive mode** (`--auto-pull` flag, or no TTY):
- If the quantization is specified (`:Q4_K_M`), download without prompting.
- If no quantization specified, **fail with error**: `"quantization required for non-interactive pull — specify model:Q4_K_M or use interactive mode"`.
- This is critical for llm-bench: benchmark configs must specify exact quantizations for reproducibility.

**Auth error propagation:** hfetch `auth.ErrAuthRequired`, `auth.ErrAuthInvalid`, and `auth.ErrGatedModel` pass through unchanged. llm-run never rewords or wraps auth errors — the user sees the same "Run `hfetch login`" guidance regardless of which tool surfaced it.

## 7. Smart Defaults

### 7.1 Hardware-Aware Defaults

On startup, `llm-run` detects hardware and derives defaults:

```go
func RecommendConfig(hw HardwareInfo, model GGUFMetadata) RunConfig {
    cfg := RunConfig{
        GPULayers:      -1,                    // Offload all layers if GPU available
        FlashAttention: true,                  // Enable if supported
        MMap:           true,                  // Memory-map by default
        MLock:          false,                 // Only on dedicated machines
    }

    // Thread count: physical cores minus 2 (leave headroom for system)
    cfg.Threads = max(1, hw.CPUCores - 2)

    // Context size: use model's trained context length, capped by available memory
    cfg.ContextSize = estimateMaxContext(hw, model)

    // Batch size: scale with available memory
    cfg.BatchSize = recommendBatchSize(hw, model)

    return cfg
}
```

### 7.2 DGX Spark Specific Tuning

When DGX Spark GB10 is detected:

- Enable all GPU layer offload (unified memory architecture).
- Set NUMA strategy to "distribute" for the 12-core Grace CPU.
- Enable flash attention (supported on SM_100).
- Increase default context length — 128 GB unified memory supports large contexts.
- Set `MLock: true` — this is a dedicated inference machine.

### 7.3 Tunable Parameters with Explanations

When the user asks to tune, provide context:

```
$ llm-run explain context-size

  Context Size (--ctx-size, -c)
  ────────────────────────────
  The maximum number of tokens the model can "see" at once, including both
  the conversation history and its response.

  Your hardware:  DGX Spark GB10, 128 GB unified memory
  Current model:  Qwen2.5-Coder-32B-Instruct (Q4_K_M, 19.9 GB)
  Free memory:    ~108 GB after model load

  Recommended:    32768 tokens  (~3.2 GB KV cache)
  Maximum:        131072 tokens (~12.8 GB KV cache)
  Current:        32768 tokens

  Higher context = more memory for KV cache, slower first-token time.
  For coding tasks, 32K is usually sufficient.
```

## 8. CLI Interface

### 8.1 Commands

```
llm-run chat <model>            Interactive chat session
  --profile <name>              Use a saved profile
  --ctx <n>                     Context size (default: auto)
  --temp <f>                    Temperature (default: 0.7)
  --system <text>               System prompt
  --system-file <path>          System prompt from file

llm-run serve <model>           Start OpenAI-compatible API server
  --profile <name>              Use a saved profile
  --host <addr>                 Bind address (default: 127.0.0.1)
  --port <n>                    Port (default: 8080)
  --ctx <n>                     Context size (default: auto)
  --parallel <n>                Parallel request slots (default: 1)
  --api-key <key>               Require API key for requests

llm-run run <model>             Single prompt, non-interactive
  --prompt <text>               Prompt text
  --prompt-file <path>          Prompt from file
  --format json                 Request JSON output
  --max-tokens <n>              Maximum tokens to generate

llm-run profile list            List saved profiles
llm-run profile show <name>     Show profile details
llm-run profile save <name>     Save current config as profile
  --model <ref>                 Model reference
  --ctx <n>                     Context size
  ... (all RunConfig fields)
llm-run profile rm <name>       Delete a profile
llm-run profile edit <name>     Edit a profile's configuration

llm-run alias set <name> <ref>  Create a model alias
llm-run alias rm <name>         Remove an alias
llm-run alias list              List all aliases

llm-run models                  List available models (local + hfetch registry)
  --local                       Only show locally available models
  --remote                      Search HuggingFace (delegates to hfetch)

llm-run hw                      Show detected hardware and recommendations
  --json                        JSON output

llm-run explain <topic>         Explain a parameter or concept
  context-size, batch-size, gpu-layers, temperature, flash-attention, ...

llm-run explain effective <model>  Show the full computed config for a model
  --profile <name>              Include profile overrides
                                Prints: resolved model path, GGUF metadata,
                                llama.cpp binary + version + capabilities,
                                full computed flag list. Reduces "it worked
                                yesterday" archaeology.

llm-run raw <model> -- <args>   Pass raw args directly to llama.cpp
                                Escape hatch for anything llm-run doesn't wrap
```

### 8.2 Interactive Chat

The chat interface provides a clean terminal experience:

```
$ llm-run chat qwen-32b

  ╭─ Qwen2.5-Coder-32B-Instruct (Q4_K_M) ─────────────────────╮
  │ Context: 32768 tokens │ GPU: GB10 │ Threads: 10            │
  ╰─────────────────────────────────────────────────────────────╯

  You: What's the most efficient way to implement a ring buffer in Go?

  Assistant: Here's a clean, generic ring buffer implementation...

  You: /stats
  ┌─ Session Stats ──────────────────────────────────────────────┐
  │ Prompt tokens:    342    │ Speed:  52.3 tok/s               │
  │ Generated tokens: 1,205  │ Context used: 4.7% (1,547/32,768)│
  └──────────────────────────────────────────────────────────────┘
```

Chat commands (prefixed with `/`):

| Command | Action |
|---|---|
| `/stats` | Show session statistics |
| `/context` | Show context window utilization |
| `/system <text>` | Set/change system prompt |
| `/save <file>` | Save conversation to file |
| `/clear` | Clear context (start fresh) |
| `/temp <f>` | Adjust temperature mid-session |
| `/quit` | Exit chat |

### 8.3 Server Mode

Launches `llama-server` with all the smart defaults. `llm-run` is **not a proxy** — it configures and launches `llama-server` as a child process, then monitors it. Clients connect directly to `llama-server`'s built-in OpenAI-compatible endpoints. The `/health` endpoint is provided by `llama-server` natively; `llm-run` uses it for health polling via `monitor.go` but does not intercept or re-expose it.

```
$ llm-run serve qwen-32b --port 8080

  ╭─ llm-run server ───────────────────────────────────────────╮
  │ Model:   Qwen2.5-Coder-32B-Instruct (Q4_K_M)             │
  │ API:     http://127.0.0.1:8080/v1                         │
  │ Context: 32768 tokens │ Parallel slots: 1                 │
  │ GPU:     GB10 (all layers offloaded)                       │
  ╰─────────────────────────────────────────────────────────────╯

  Endpoints (served by llama-server):
    POST /v1/chat/completions    (OpenAI-compatible)
    POST /v1/completions         (OpenAI-compatible)
    GET  /v1/models              (OpenAI-compatible)
    GET  /health                 (llama-server health check)

  Press Ctrl+C to stop.
```

`pkg/api` is a Go HTTP client for these endpoints — used by the `chat` command to talk to the server it launched, and by `llm-bench` to send benchmark prompts. It is not middleware.

## 9. Profile System

### 9.1 Profile Storage

Profiles stored as JSON in `$LLM_RUN_CONFIG_DIR/profiles/`:

```json
{
  "name": "coding",
  "description": "Qwen 32B optimized for code generation",
  "config": {
    "modelRef": "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M",
    "contextSize": 32768,
    "temperature": 0.3,
    "topP": 0.9,
    "flashAttention": true,
    "gpuLayers": -1,
    "systemPrompt": "You are a senior software engineer. Write clean, idiomatic code."
  },
  "createdAt": "2026-02-27T23:00:00Z",
  "updatedAt": "2026-02-27T23:00:00Z",
  "lastUsedAt": "2026-02-27T23:30:00Z"
}
```

### 9.2 Built-in Profiles

Ship with sensible defaults for common use cases:

| Profile | Description | Key Settings |
|---|---|---|
| `default` | General-purpose conversation | temp=0.7, ctx=auto |
| `coding` | Code generation | temp=0.3, topP=0.9 |
| `creative` | Creative writing | temp=0.9, topP=0.95 |
| `precise` | Factual/analytical | temp=0.1, topP=0.8 |

Users override or extend these with their own profiles.

## 10. Process Management

### 10.1 Server Lifecycle

When running in server mode:

- Launch `llama-server` as a child process.
- Monitor health via the `/health` endpoint.
- Graceful shutdown on SIGINT/SIGTERM: wait for in-flight requests to complete.
- Automatic restart on crash (configurable, default off).
- PID file written to `$LLM_RUN_DATA_DIR/server.pid` to prevent double-launch.
- If PID file exists but the process is dead, clean up the stale PID file and continue with launch.
- If PID file exists and the process is alive, error with "server already running on port <N>" and show the existing endpoint for reuse.

### 10.2 Resource Monitoring

Optionally display real-time metrics while the server is running:

- Tokens per second (prompt processing and generation).
- Context utilization across parallel slots.
- Memory usage (RSS, GPU memory).
- Request count and latency.

## 11. Go Library API

```go
package llmrun

// Engine manages llama.cpp processes
type Engine struct { ... }

// NewEngine creates a new engine, detecting llama.cpp and hardware
func NewEngine(opts ...Option) (*Engine, error)

// Launch starts an inference process with the given config
func (e *Engine) Launch(ctx context.Context, cfg RunConfig) (*Process, error)

// Process represents a running llama.cpp instance
type Process struct { ... }
func (p *Process) Wait() error
func (p *Process) Stop() error
func (p *Process) Health() (HealthStatus, error)
func (p *Process) Endpoint() string  // API endpoint when in server mode
func (p *Process) Stats() ProcessStats

// ResolveModel resolves a model reference to a structured result with
// full provenance. See section 6.3 for ResolvedModel fields.
func (e *Engine) ResolveModel(ctx context.Context, ref string) (*ResolvedModel, error)

// DetectHardware returns information about the current system
func DetectHardware() (*HardwareInfo, error)

// DetectCapabilities probes the llama.cpp installation and returns
// its supported features. See section 5.2 for Capabilities fields.
func (e *Engine) DetectCapabilities() (*Capabilities, error)

// BuildCommand translates a RunConfig into a command line, gated by capabilities.
// Returns the command args and a list of warnings for degraded features.
func BuildCommand(cfg RunConfig, caps Capabilities) (cmd []string, warnings []string, err error)

// Recommend returns a RunConfig with smart defaults for the given model and hardware
func Recommend(hw *HardwareInfo, model *hfetch.GGUFMetadata) RunConfig

// ProfileStore manages saved profiles
type ProfileStore struct { ... }
func NewProfileStore(configDir string) *ProfileStore
func (ps *ProfileStore) List() ([]Profile, error)
func (ps *ProfileStore) Get(name string) (*Profile, error)
func (ps *ProfileStore) Save(p Profile) error
func (ps *ProfileStore) Delete(name string) error
```

## 12. Integration Points

### 12.1 With hfetch

- Imports `hfetch/pkg/registry` for model resolution and path lookup.
- Imports `hfetch/pkg/gguf` for model metadata (parameter count, architecture, context length).
- Imports `hfetch/pkg/config` for **default token resolution** — llm-run never stores HF credentials. A single `hfetch login` authenticates the entire toolchain. llm-run may pass through an explicit `--token` override to `hfetch.WithToken()` for CI use.
- Imports `hfetch/pkg/auth` for typed auth errors — surfaces `auth.ErrAuthRequired`, `auth.ErrGatedModel` etc. with consistent guidance ("Run `hfetch login`").
- Can trigger `hfetch` downloads when models are missing locally (using the shared token for gated model access).

### 12.2 With Benchmark Suite (llm-bench)

The benchmark suite uses `llm-run` as its inference engine:

```go
// llm-bench creates RunConfigs programmatically:
engine, _ := llmrun.NewEngine()

cfg := llmrun.RunConfig{
    ModelRef:    "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M",
    ServerMode:  true,
    Port:        randomPort(),
    ContextSize: 4096,  // Fixed for benchmark consistency
}

proc, _ := engine.Launch(ctx, cfg)
defer proc.Stop()

// Run benchmarks against proc.Endpoint()
```

## 13. Directory Layout

Mirrors hfetch's XDG separation:

```
LLM_RUN_CONFIG_DIR/                    # Default: ~/.config/llm-run
├── profiles/                          # Saved profile JSON files
├── aliases.json                       # User-defined model aliases
└── config.json                        # Preferences, defaults

LLM_RUN_DATA_DIR/                      # Default: ~/.local/share/llm-run
├── server.pid                         # PID file for running server
└── logs/                              # Run logs, captured stderr

LLM_RUN_CACHE_DIR/                     # Default: ~/.cache/llm-run
└── caps/                              # Cached llama.cpp capability probes
```

`LLM_RUN_HOME` is a convenience override that remaps all three (same semantics as `HFETCH_HOME`). Individual `LLM_RUN_CONFIG_DIR`, `LLM_RUN_DATA_DIR`, `LLM_RUN_CACHE_DIR` override per-role.

## 14. Environment Variables

| Variable | Purpose | Default |
|---|---|---|
| `LLM_RUN_HOME` | Convenience override — remaps config/data/cache subdirs | (none, uses XDG) |
| `LLM_RUN_CONFIG_DIR` | Config directory (profiles, aliases, settings) | `~/.config/llm-run` |
| `LLM_RUN_DATA_DIR` | Data directory (PID files, logs) | `~/.local/share/llm-run` |
| `LLM_RUN_CACHE_DIR` | Cache directory (capability probes) | `~/.cache/llm-run` |
| `LLM_RUN_LLAMA_DIR` | Path to llama.cpp build/bin directory | auto-detect |
| `LLM_RUN_DEFAULT_MODEL` | Default model when none specified | (none) |
| `LLM_RUN_DEFAULT_PROFILE` | Default profile to apply | `default` |

## 15. Error Handling

- **llama.cpp not found:** Clear installation instructions, suggest common paths, show `LLM_RUN_LLAMA_DIR` hint.
- **Model not found:** Show closest matches from registry and aliases. Suggest `llm-run models --local`.
- **Auth errors during auto-pull:** Pass through `auth.ErrAuthRequired` / `auth.ErrAuthInvalid` / `auth.ErrGatedModel` from hfetch unchanged. Never reword — user sees "Run `hfetch login`" consistently.
- **Out of memory:** Estimate memory requirement vs. available, suggest smaller quantization.
- **Port in use:** Auto-select next available port, or show what's using the requested port.
- **GPU not detected:** Fall back to CPU with a warning and performance estimate.
- **Capability mismatch:** Graceful degradation for performance flags (warn + disable), hard error for structural flags (GPU on CPU build). See section 5.3 for the full gating table.
- **Crash during inference:** Capture stderr from llama.cpp, display with context about likely causes.

## 16. Testing Strategy

- **Unit tests:** Config translation, model resolution, profile management, hardware recommendation logic, capability gating.
- **Integration tests:** Launch llama.cpp with a tiny test model (e.g., TinyLlama 1.1B GGUF), verify API responses.
- **Mock tests:** Mock the llama.cpp binary to test process management without actual inference. Mock capability probes to test degradation paths.
- **Hardware detection tests:** Parseable on any machine (graceful degradation when GPU not present).

## 17. Future Considerations

- **Multi-model serving:** Running multiple models on different ports (memory permitting).
- **Speculative decoding:** Support for draft model configurations when llama.cpp adds it.
- **LoRA adapter management:** Loading and switching LoRA adapters at runtime.
- **Prompt template management:** Model-specific chat templates with auto-detection from GGUF metadata.
- **Remote server support:** Managing llama.cpp instances on other machines (e.g., headless DGX).
