# Internal Seam Test Audit

First-pass audit of test seams for extracting backend behavior into
`/internal/*` packages. This document records the current test baseline and the
missing tests needed before refactors.

## Validation Commands

Full suite:

```text
go test ./...
```

Result: passed. No failures.

Coverage:

```text
go test -cover ./...
```

Result: passed.

## Coverage Snapshot

Relevant command and shared package coverage from `go test -cover ./...`:

| Package | Coverage |
| --- | ---: |
| `cmd/hfetch` | 7.4% |
| `cmd/llm-run` | 0.0% |
| `cmd/llm-serve` | 0.0% |
| `cmd/llm-tidy` | 5.2% |
| `internal/progress` | 0.0% |
| `internal/tui` | 3.3% |
| `internal/ui` | 0.0% |
| `pkg/hfetch` | 0.0% |
| `pkg/hfetch/api` | 78.8% |
| `pkg/hfetch/config` | 86.9% |
| `pkg/hfetch/download` | 85.3% |
| `pkg/hfetch/fileset` | 89.0% |
| `pkg/hfetch/gguf` | 71.8% |
| `pkg/hfetch/quant` | 97.1% |
| `pkg/hfetch/registry` | 85.8% |
| `pkg/hfetch/source` | 25.0% |
| `pkg/llmrun` | 0.0% |
| `pkg/llmrun/api` | 77.0% |
| `pkg/llmrun/config` | 84.1% |
| `pkg/llmrun/engine` | 37.4% |
| `pkg/llmrun/hardware` | 68.4% |
| `pkg/llmrun/profiles` | 83.3% |
| `pkg/llmrun/resolver` | 84.4% |
| `pkg/llmserve` | 62.7% |
| `pkg/llmserve/artifact` | 74.0% |
| `pkg/llmserve/contract` | 76.9% |
| `pkg/llmserve/emit` | 76.3% |
| `pkg/llmserve/fingerprint` | 81.8% |
| `pkg/llmserve/instance` | 74.7% |
| `pkg/llmserve/lifecycle` | 71.5% |
| `pkg/llmserve/liveness` | 80.4% |
| `pkg/llmserve/profiles` | 36.4% |
| `pkg/llmserve/runtime` | 30.0% |
| `pkg/llmserve/serving` | 0.0% |
| `pkg/llmtidy` | 41.6% |
| `pkg/llmtidy/interlock` | 85.7% |
| `pkg/llmtidy/inventory` | 48.7% |
| `pkg/llmtidy/manifest` | 89.7% |
| `pkg/llmtidy/ollama` | 81.8% |
| `pkg/llmtidy/reconcile` | 78.4% |
| `pkg/seam` | no statements |

Out-of-scope binaries also ran:

- `cmd/llm-bench`: 0.0% command coverage, package tests under `pkg/llmbench/*`
  passed.
- `cmd/llm-chat`: 0.0% command coverage.

## Commands With No Direct Seam Coverage

- `cmd/llm-run`: no tests. Uncovered command seams include `chat`, `run`,
  `serve`, `raw`, `profile`, `alias`, `models`, `hw`, and `explain effective`.
- `cmd/llm-serve`: no tests. Uncovered command seams include `emit`, `up`,
  `down`, `status`, `recover`, `forget`, and `liveness --check`.
- `cmd/hfetch`: low coverage. Covered helpers include selected pull validation,
  destination expansion, completeness reporting, `fetchQuantInfo`, and
  `verifyOne`. Uncovered command paths include auth, search, files, list, path,
  rm, gc, config, bare-arg shorthand, full `runPull`, JSON progress, picker, and
  `ollama-import`.
- `cmd/llm-tidy`: low coverage. Covered helpers are `parseDuration` and
  `humanAge`. Uncovered command paths include root construction, manifest flag
  wiring, status rendering, prune interlock behavior, sync dry-run/execute,
  promote, demote, and init.
- Facades `pkg/hfetch` and `pkg/llmrun` have 0.0% coverage despite being the
  intended import surface for downstream tools.

## Seam Audit

### Docker, Podman, Quadlet Interactions

Current files and functions:

- `pkg/llmserve/emit/emit.go`: `DockerRun`, `Compose`, `Quadlet`, `Render`,
  `SpecHash`, `planLaunch`, `containerPath`.
- `pkg/llmserve/runtime/compose.go`: `Compose.Up`, `Down`, `Inspect`,
  `ListRunning`, `dockerPS`, `inspectContainers`.
- `pkg/llmserve/lifecycle/*.go`: `Orchestrator.Up/Down/Forget/Recover/Status`,
  `Reconcile`, `IdentityLabels`.
- `pkg/llmserve/liveness/liveness.go`: `Protected`, `FilterProtected`,
  `Protects`, `Instance`.
- `cmd/llm-serve/emit.go`, `lifecycle.go`, `liveness.go`.
- `pkg/llmtidy/interlock/shellout.go`: `LLMServeChecker`.

Shared or command-specific:

- Shared library logic exists under `pkg/llmserve`.
- Command-specific parsing and output are still in `cmd/llm-serve`.
- `llm-tidy` consumes the liveness contract via a shell-out.

External side effects:

- Runs `docker compose up -d`, `docker compose down`, `docker ps`,
  `docker inspect`.
- Writes compose specs, instance manifests, lock files, and watchdog scripts.
- Reads bind mounts and labels from running containers.
- Exposes stdin/stdout protocol through `llm-serve liveness --check`.

Current tests:

- `pkg/llmserve/emit`: compose/docker-run/quadlet rendering, quoting,
  deterministic labels, warnings, YAML round trip.
- `pkg/llmserve/plan`: labels, watchdog, absolute mounts, bad name rejection.
- `pkg/llmserve/lifecycle`: fake runtime/prober for up, down, replace, recover,
  conflicts, crash loops, missing watchdog, fail-closed paths.
- `pkg/llmserve/liveness`: manifest intent, running managed/unmanaged
  containers, path overlap, symlink canonicalization, fail-closed cases.
- `pkg/llmserve/runtime`: HTTP prober only.
- `pkg/seam`: emit labels protect artifacts.

Missing tests:

- No `cmd/llm-serve` command tests for flags, stdout/stderr, `--repo-tree`,
  `--check`, or `--protected-artifacts`.
- No fake command runner tests for `runtime.Compose` argument construction.
- No Docker/Compose integration tests, even behind build tags.
- No Podman/Quadlet runtime integration. Quadlet is render-only.
- No test proving `llm-tidy` shell-out and `llm-serve liveness --check` remain
  byte-for-byte compatible across path spacing and warnings.

Suggested package:

- `internal/servespec`, `internal/servehost`, `internal/eviction`.

Public-ish internal API:

- `servespec.Render(target Target, resolved Resolved, host Host) (string, error)`.
- `servehost.Runtime` and `servehost.Prober` interfaces, with Docker/Compose
  implementation behind injected `process.Runner`.
- `eviction.FilterProtected(ctx, candidates []string)`.

### Model Discovery, Registry, Metadata

Current files and functions:

- `pkg/hfetch/api.Client`: `Search`, `GetModel`, `ListFiles`, `HeadFile`,
  `FetchFileRange`, `DownloadFile`.
- `pkg/hfetch/source.File`: tree-listing backed `download.FileSource`.
- `pkg/hfetch/registry.Registry`: `Load`, `Save`, `List`, `Get`, `Path`,
  `AddFile`, `Remove`, `ModelDir`, `GC`.
- `pkg/hfetch/gguf`, `pkg/hfetch/quant`, `pkg/hfetch/fileset`.
- `pkg/llmrun/resolver.Resolver`: `ResolveModel`, aliases, local path and
  hfetch registry refs.
- `pkg/llmserve/artifact`: `Verify`, `DetectFacts`.
- `pkg/llmtidy/inventory`: `OllamaList`, `GGUFList`, `VLLMList`, delete paths.
- `cmd/hfetch/pull.go`, `cmd/hfetch/info.go`, `cmd/hfetch/files.go`,
  `cmd/llm-run/models.go`.

Shared or command-specific:

- Most lower-level behavior is shared.
- `cmd/hfetch/pull.go` still owns important selection and verification behavior
  not present in `pkg/hfetch.Client.Pull`.
- `cmd/llm-run/models.go` scans the filesystem directly instead of using the
  registry API.

External side effects:

- HuggingFace HTTP requests.
- Registry manifest reads/writes.
- Local file stat/open/read/writes for GGUF, configs, tokenizers, and model
  store paths.

Current tests:

- `pkg/hfetch/api`: search, model metadata, tree pagination, auth errors,
  retries, HEAD, range fetch, download streams, cache.
- `pkg/hfetch/source`: source uses injected metadata for `Head`.
- `pkg/hfetch/registry`: load/save, remove, path, storage layout, GC.
- `pkg/hfetch/fileset`: serve-ready completeness gate, tokenizer/config/weights,
  hash mismatches, auto_map modules, non-LFS git blob checks.
- `pkg/hfetch/gguf`: parse/filter/merge/fit.
- `pkg/hfetch/quant`: quant config parsing.
- `pkg/llmrun/resolver`: local paths, aliases, registry refs, `hf://`, missing
  and incomplete registry files.
- `pkg/llmserve/artifact`: architecture, tokenizer, quant, remote code, vision.
- `pkg/llmtidy/inventory`: Ollama/GGUF/vLLM list and delete behavior.
- `pkg/seam`: HF tree protocol, library pull tree-listing authority, registry
  inventory separation, serve artifact completeness.

Missing tests:

- No facade coverage for `pkg/hfetch.Client.Pull`.
- No test proving CLI `runPull` and library `Client.Pull` have the same registry
  and source behavior.
- No command-level tests for `hfetch pull --profile vllm`, `--dest vllm`,
  picker-less explicit filenames, JSON progress, or custom output plus registry
  path correctness.
- No direct test for `cmd/llm-run scanLocalModels` against registry edge cases.

Suggested package:

- `internal/hub`, `internal/modelstore`, `internal/modelmeta`,
  `internal/modelref`, `internal/fileset`, `internal/gguf`.

Public-ish internal API:

- `hub.Client.ListFiles(ctx, repo) ([]File, error)`.
- `modelstore.Registry.AddFile/Get/List/Remove`.
- `modelmeta.VerifyServeReady(repoFiles, dir) (Facts, error)`.
- `modelref.Resolver.Resolve(ctx, ref) (Resolved, error)`.

### Runtime Execution

Current files and functions:

- `pkg/llmrun/engine.DetectBinaries`, `BuildCommand`, `Launch`,
  `WaitForReady`, `Process.Stop`, `CrashLog`.
- `cmd/llm-run/chat.go`: `runInference`.
- `cmd/llm-run/serve.go`, `run.go`, `raw.go`.
- `pkg/llmserve/runtime.Compose`.
- `cmd/hfetch/ollama.go`: `runOllamaImport`.

Shared or command-specific:

- `llmrun/engine` is shared, but command orchestration is command-specific.
- `raw` and `ollama-import` execute processes directly from command packages.

External side effects:

- Searches PATH, runs version/help probes, launches long-lived processes.
- Writes PID and log files.
- Sends SIGTERM/SIGKILL.
- Polls HTTP health endpoints.
- Forwards stdio for raw mode and `ollama create`.

Current tests:

- `pkg/llmrun/engine`: command building, capability degradation, hard errors,
  parser helpers, fake process wait/stop/reaper.
- `pkg/llmrun/api`: health, model list, chat/completion, streaming, API key.
- `pkg/llmserve/runtime`: HTTP prober.

Missing tests:

- No command tests for `runInference`, `serve`, `run`, or `raw`.
- No injected launcher/prober seam for high-level `llm-run` flows.
- No real `Launch` integration test behind build tag.
- No tests for PID-file behavior on live OS processes.
- No tests for `hfetch ollama-import` command execution and temp file cleanup.

Suggested package:

- `internal/llamacpp`, `internal/inference`, `internal/process`.

Public-ish internal API:

- `llamacpp.Detect(ctx, opts) (Installation, error)`.
- `llamacpp.Build(config, caps) (Command, []Warning, error)`.
- `inference.Chat/Run/Serve(ctx, Request) error`.
- `process.Runner` for `LookPath`, `Output`, `Run`, and `Start`.

### Config Loading and Defaults

Current files and functions:

- `pkg/hfetch/config.Dirs`, `ResolveToken`, `StoreToken`, `ClearToken`.
- `cmd/hfetch/config.go`: `loadPrefs`, `savePrefs`.
- `pkg/llmrun/config.Dirs`, `LoadGlobalConfig`, `SaveGlobalConfig`.
- `pkg/llmrun/profiles.ProfileStore`.
- `pkg/llmtidy/manifest.Resolve`, `ConfigDir`, `Load`, `Save`, `Validate`.
- `cmd/llm-serve/lifecycle.go:dirs`.
- `pkg/llmrun/hardware.RecommendConfig`, command `applyDefaults`.

Shared or command-specific:

- Per-tool config is shared inside each tool, but cross-tool path/default
  resolution is duplicated.
- hfetch command preferences are command-local and not used by the library.

External side effects:

- Reads environment variables and user home.
- Reads/writes token files, config JSON, profile JSON, manifest YAML.

Current tests:

- `pkg/hfetch/config`: dirs and token precedence.
- `pkg/llmrun/config`: dirs, env overrides, save/load.
- `pkg/llmrun/profiles`: built-ins, save/get/list/delete, built-in override.
- `pkg/llmtidy/manifest`: resolve precedence, load/save, validation.
- `pkg/llmrun/hardware`: recommendations and DGX defaults.

Missing tests:

- No `cmd/hfetch config` tests.
- No `llm-serve` dir-resolution tests.
- No tests for command flag precedence over defaults in `llm-run chat/run/serve`.
- No cross-tool test that hfetch token/config resolution is consistent where
  downstream tools use hfetch.

Suggested package:

- `internal/paths`, `internal/hftoken`, `internal/runconfig`,
  `internal/tidymanifest`.

Public-ish internal API:

- `paths.For(app App, env Env) Dirs`.
- `hftoken.Resolve(override string) Token`.
- `runconfig.Load/Save`, `runconfig.Profiles`.
- `tidymanifest.Resolve/Load/Validate/Save`.

### Filesystem, State, Cache Paths

Current files and functions:

- `pkg/hfetch/download.Download`, `LoadState`, `SaveState`.
- `pkg/hfetch/registry.Registry` and `StorageLayout`.
- `pkg/hfetch/api/cache.go`.
- `pkg/llmserve/instance.Store`.
- `pkg/llmserve/llmserve.EnsureWatchdogScript`.
- `pkg/llmrun/engine.Launch`, `profiles`, `resolver` aliases.
- `pkg/llmtidy/manifest`.

Shared or command-specific:

- State stores are shared by tool, but path derivation is split.
- Some command paths bypass registries and walk files directly.

External side effects:

- Creates directories.
- Writes JSON/YAML manifests, cache files, temp files, partial files, state
  files, PID files, logs, and locks.
- Removes model files, model directories, old specs, partial files, and merged
  GGUF files.

Current tests:

- Download state, resume, rename, cancellation, disk space, range fallback.
- Registry manifest, storage layout, remove, GC.
- Instance store atomic save/list/delete/lock/name validation.
- Manifest load/save/validate.
- Profile and alias file tests.

Missing tests:

- No unified path authority tests across all tools.
- No command smoke tests that set all XDG and tool-specific env vars and verify
  files land under temp dirs.
- No test for `llm-run` PID/log path behavior through command flows.
- No test for hfetch custom output plus registry path plus vLLM liveness path
  compatibility beyond targeted unit tests.

Suggested package:

- `internal/paths`, `internal/modelstore`, `internal/download`,
  `internal/instances`.

Public-ish internal API:

- `paths.AppDirs`.
- `modelstore.Layout` and `Registry`.
- `download.Download`.
- `instances.Store`.

### HTTP, Download, Fetch Behavior

Current files and functions:

- `pkg/hfetch/api.Client.do`, `ListFiles`, `HeadFile`, `FetchFileRange`,
  `DownloadFile`.
- `pkg/hfetch/download.Download`.
- `pkg/hfetch/source.File`.
- `pkg/llmtidy/ollama.Client`.
- `pkg/llmrun/api.Client`.
- `pkg/llmserve/runtime.HTTPProber`.
- `cmd/hfetch/info.go:fetchQuantInfo`, `cmd/hfetch/verify.go:verifyOne`.

Shared or command-specific:

- Most HTTP clients are shared packages.
- Client construction and some fetch workflows remain command-specific.

External side effects:

- Network requests, retries/backoff, streamed bodies, range requests.
- File writes during downloads.
- HTTP calls to local Ollama, llama-server, and vLLM endpoints.

Current tests:

- HuggingFace API client coverage for auth, cache, pagination, retries, HEAD,
  range, download, and errors.
- Download manager coverage for parallel streams, rate limiting, disk space,
  cancellation, range fallback.
- Ollama client tests through `httptest`.
- llama-server API client tests through `httptest`.
- vLLM prober tests through `httptest`.
- hfetch `verifyOne` and `fetchQuantInfo` tests.

Missing tests:

- No package coverage for `pkg/hfetch.Client.Pull`.
- No test for retry backoff without real sleeping via injected clock.
- No command-level tests for token flag/env/config propagation into API client.
- No streaming/TUI integration seam test for `llm-run chat`.

Suggested package:

- `internal/hub`, `internal/download`, `internal/ollama`,
  `internal/openaiapi`.

Public-ish internal API:

- Domain clients with injected `*http.Client`.
- Download source adapters that do not issue metadata HEAD requests when the
  tree listing is authoritative.

### Logging and Output Formatting

Current files and functions:

- Text/JSON renderers in `cmd/hfetch`, `cmd/llm-run`, `cmd/llm-serve`,
  `cmd/llm-tidy`.
- `internal/progress.FormatSize`, `FormatSpeed`, `Bar`.
- `internal/tui.RenderServerHeader`, `RenderServerEndpoints`, `RunChat`.
- `internal/ui.PickGGUFFile`, `Confirm`.

Shared or command-specific:

- Mostly command-specific.
- A few formatting helpers are already internal.

External side effects:

- stdout/stderr output.
- Interactive forms and terminal UI.
- JSON progress events.

Current tests:

- `internal/tui` boundary stripping tests.
- `cmd/llm-tidy` helper tests for duration/age.
- hfetch helper tests for completeness output indirectly.

Missing tests:

- No golden tests for command output, JSON progress, status JSON, liveness
  output, or server headers.
- No tests for `internal/progress`.
- No tests for `internal/ui` confirm/picker behavior with injected UI.
- No tests that stdout and stderr remain separated for pipeable commands like
  `llm-serve emit`.

Suggested package:

- `internal/output`, `internal/progress`, `internal/chatui`, `internal/picker`.

Public-ish internal API:

- Renderer functions that take `io.Writer`.
- Interactive UI interfaces so commands can be tested without real terminal
  interaction.

### Process Execution and Shell Boundaries

Current files and functions:

- `pkg/llmrun/engine/detect.go`: `exec.LookPath`, `exec.Command`.
- `pkg/llmrun/engine/launch.go`: `exec.CommandContext`, signals.
- `pkg/llmrun/hardware/detect.go`: `cat`, `sysctl`, `vm_stat`, `nvidia-smi`,
  `ls`, `lscpu`.
- `pkg/llmserve/runtime/compose.go`: `docker`, `docker compose`.
- `pkg/llmtidy/interlock/shellout.go`: `llm-serve liveness --check`.
- `cmd/hfetch/ollama.go`: `exec.LookPath("ollama")`, `ollama create`.
- `cmd/llm-run/raw.go`: direct raw process execution.

Shared or command-specific:

- Mixed. Some process calls are shared; several high-risk process calls are in
  command files.

External side effects:

- Host process execution, PATH lookup, stdin/stdout/stderr forwarding, signals,
  process groups by implication, Docker daemon access, Ollama CLI access.

Current tests:

- Parser/helper tests for llama.cpp detection.
- Fake process-handle tests for wait/stop.
- Interlock shell-out tests for missing binary behavior and path splitting.
- No direct tests for hardware command execution beyond parser functions and
  best-effort detection.

Missing tests:

- No common command runner seam.
- No tests for generated process args at Docker/Ollama/raw boundaries.
- No tests for stderr propagation and exit-code wrapping in Compose and
  `ollama create`.
- No tests for signal behavior in `llm-run serve` and `raw`.

Suggested package:

- `internal/process`, consumed by `internal/llamacpp`, `internal/servehost`,
  `internal/ollama`, and `internal/eviction`.

Public-ish internal API:

- `Runner.LookPath`, `Runner.Output`, `Runner.Run`, `Runner.Start`.
- Captured command result type with stdout, stderr, and exit error.
- Real runner plus fake runner for tests.

## Deferred Gaps (hermetic testing not possible)

These seams cannot be tested without a real TTY, Docker/Podman, systemd/Quadlet,
or network host services. Each entry records why it is not hermetic, what
behavior stays uncovered, and the future integration test that should cover it.

- **`internal/tui` chat loop (`RunChat`, `Init`, `Update`, `View`,
  `streamResponse`).** Why: drives a `charmbracelet/bubbletea` program against an
  alt-screen TTY, and `streamResponse` performs live SSE streaming against a
  llama-server. Covered hermetically instead: all `/`-slash command behavior
  (`handleSlashCommand`), `saveConversation` file output, boundary/strip token
  logic, and the status renderers. Uncovered: key-event routing, viewport
  rendering, live token streaming. Future: a bubbletea `teatest` golden-frame
  test plus an `httptest` SSE server feeding `streamResponse`.
- **`internal/ui` forms (`PickGGUFFile`, `Confirm`).** Why: `huh` `form.Run()`
  blocks on interactive terminal input. Covered hermetically: the empty-items
  guard in `PickGGUFFile`. Uncovered: option-label construction and selection
  result. Future: extract an injectable UI interface (`internal/picker`) so the
  command layer can drive a scripted fake, per this audit's suggested API.

## Overall Test Readiness

Ready to extract with low risk:

- Quant parsing, fileset completeness, registry storage, manifest validation,
  hfetch API tree/listing behavior, llmserve contract resolution, emit rendering,
  liveness core logic, and tidy reconcile planning.

Needs tests before extraction:

- Command orchestration for `llm-run` and `llm-serve`.
- The hfetch CLI/library pull parity.
- Real process boundary adapters with injected runners.
- Cross-tool eviction contract between `llm-tidy` and `llm-serve`.
- Output contracts for pipeable commands and JSON modes.
