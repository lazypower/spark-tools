# Internal Extraction Map

First-pass audit for extracting backend behavior into `/internal/*` library
components. This document maps ownership and target package boundaries only. No
code has been moved.

## Command Map

Other binaries exist in this repository (`llm-bench`, `llm-chat`) but are not in
the requested extraction scope. They still matter as shared-consumer checks:
`llm-chat` uses `internal/tui`, and `llm-bench` uses the same package-level
facade pattern as the requested tools.

### `llm-tidy`

Entrypoint: `cmd/llm-tidy/main.go`.

Commands:

- `status`: `cmd/llm-tidy/status.go`, calls `runStatus`, `Tidy.Provider().Probe`,
  `Tidy.LoadManifest`, `Tidy.Inventory`, `reconcile.Diff`, then renders text or
  JSON.
- `prune`: `cmd/llm-tidy/prune.go`, calls `runPrune`,
  `pruneBuildPlan`, `interlock.Apply`, `ui.Confirm`, and `reconcile.Prune`.
- `sync`: `cmd/llm-tidy/sync.go`, calls `runSync`, `reconcile.SyncPlan`, then
  `Tidy.Sync`.
- `promote`: `cmd/llm-tidy/promote.go`, calls `Tidy.Promote`.
- `demote`: `cmd/llm-tidy/demote.go`, calls `Tidy.Demote`.
- `init`: `cmd/llm-tidy/init.go`, calls `Tidy.Init`.

Shared command helpers:

- `newTidy`, `newTidyWithOverride`: resolve persistent manifest/Ollama flags and
  construct `pkg/llmtidy.Tidy`.
- `resolveBackend`, `parseDuration`, `modelsBy`, `humanAge`, `formatSize`:
  `cmd/llm-tidy/util.go`.

Hidden/shared paths:

- Eviction safety is also enforced in `pkg/llmtidy.Tidy.Prune`, but the CLI
  `runPrune` currently repeats the interlock and then calls `reconcile.Prune`
  directly. That is a competing safety path to collapse before extraction.
- The interlock shells out to `llm-serve liveness --check` through
  `pkg/llmtidy/interlock.LLMServeChecker`.

### `llm-serve`

Entrypoint: `cmd/llm-serve/main.go`.

Commands:

- `emit`: `cmd/llm-serve/emit.go`, resolves artifact facts, parses caps/mounts,
  calls `llmserve.Emit`, writes warnings to stderr and spec to stdout.
- `profiles`: `cmd/llm-serve/info.go`, lists built-in architecture profiles.
- `targets`: `cmd/llm-serve/info.go`, lists render targets.
- `up`: `cmd/llm-serve/lifecycle.go`, resolves artifact facts, writes watchdog,
  builds a lifecycle plan, calls `Orchestrator.Up`.
- `down`: `cmd/llm-serve/lifecycle.go`, calls `Orchestrator.Down`.
- `status`: `cmd/llm-serve/lifecycle.go`, calls `Orchestrator.Status/List`.
- `recover`: `cmd/llm-serve/lifecycle.go`, calls `Orchestrator.Recover`.
- `forget`: `cmd/llm-serve/lifecycle.go`, calls `Orchestrator.Forget`.
- `liveness`: `cmd/llm-serve/liveness.go`, reports protected artifacts and
  exposes the machine-readable `--check` contract.

Shared command helpers:

- `resolveFacts`, `loadRepoTree`, `parseCaps`, `parseMounts`, `parseTarget`,
  `imageRef`: `cmd/llm-serve/emit.go`.
- `dirs`: `cmd/llm-serve/lifecycle.go`, resolves `LLM_SERVE_HOME` or
  `XDG_STATE_HOME`.
- `readLines`, `printUnmanaged`, `sortedKeys`: `cmd/llm-serve/liveness.go`.

Hidden/shared paths:

- `liveness --check` reads candidate paths from stdin and prints only protected
  paths to stdout. This is the stable inter-tool contract used by `llm-tidy`.
- `up` accepts `--target` but currently rejects non-`compose` targets for B1.
- `emit` supports `compose`, `docker-run`, and `quadlet` render targets.

### `llm-run`

Entrypoint: `cmd/llm-run/main.go`.

Commands:

- `chat`: `cmd/llm-run/chat.go`, launches llama-server, waits for readiness,
  then starts the chat TUI.
- `run`: `cmd/llm-run/run.go`, reuses `runInference` for one prompt.
- `serve`: `cmd/llm-run/serve.go`, launches llama-server and blocks until signal
  or unexpected process exit.
- `profile list/show/save/edit/rm`: `cmd/llm-run/profile.go`, manages saved
  `engine.RunConfig` JSON profiles.
- `alias set/rm/list`: `cmd/llm-run/alias.go`, manages `aliases.json`.
- `models`: `cmd/llm-run/models.go`, scans local hfetch data; `--remote` only
  tells the user to run `hfetch search`.
- `hw`: `cmd/llm-run/hw.go`, runs hardware detection and recommendation output.
- `explain`: `cmd/llm-run/explain.go`, prints static help or computed effective
  config.
- `raw`: `cmd/llm-run/raw.go`, resolves a model and forwards raw args to
  `llama-cli` or `llama-server`.

Shared command helpers:

- `resolveModelArg`, `runInference`, `applyDefaults`, `gpuName`,
  `formatCrashLog`, `printServerHeader`: `cmd/llm-run/chat.go`.
- `lookupRawBinary`: `cmd/llm-run/raw.go`.
- `scanLocalModels`, command-local `formatSize`: `cmd/llm-run/models.go`.

Hidden/shared paths:

- `raw <model> -- <args>` is the explicit shell/process escape hatch.
- `resolver.ResolveModel` recognizes `hf://...` as a HuggingFace-intent source,
  but does not download; callers still surface "use hfetch pull".
- `chat`, `run`, and `serve` duplicate orchestration for config, profile,
  resolver, hardware defaults, binary detection, launch, readiness, and crash
  formatting.

### `hfetch`

Entrypoint: `cmd/hfetch/main.go`.

Commands:

- Bare arg shorthand: `hfetch org/model` dispatches to `runPull` when the single
  arg contains `/`.
- `search`: `cmd/hfetch/search.go`, calls HuggingFace search.
- `info`: `cmd/hfetch/info.go`, calls model metadata, optional file listing,
  and quant config probes.
- `files`: `cmd/hfetch/files.go`, lists and groups repo files.
- `pull`: `cmd/hfetch/pull.go`, selects GGUF or vLLM files, downloads them,
  registers them, and optionally runs completeness verification.
- `verify`: `cmd/hfetch/verify.go`, reruns the serve-ready completeness gate.
- `list`: `cmd/hfetch/list.go`, renders the local registry.
- `path`: `cmd/hfetch/list.go`, prints a registered local path.
- `rm`: `cmd/hfetch/manage.go`, removes registry entries and files.
- `gc`: `cmd/hfetch/manage.go`, removes partial/orphaned files.
- `login/logout/whoami`: `cmd/hfetch/auth.go`, validates and stores/clears HF
  tokens.
- `config`, `config set`, `config get`: `cmd/hfetch/config.go`, shows or writes
  command-local preferences.
- `ollama-import`: `cmd/hfetch/ollama.go`, resolves local GGUF, optionally
  merges shards, emits a Modelfile, and runs `ollama create`.

Shared command helpers:

- `tokenFlag`, `resolveToken`, `newAPIClient`, `formatSize`:
  `cmd/hfetch/main.go`.
- `resolveDest`, `validateSelected`, `checkFlatCollisions`, `resolveStreams`,
  `reportCompleteness`, `parseBandwidth`: `cmd/hfetch/pull.go`.
- `redactToken`, `tokenSourceLabel`: `cmd/hfetch/pull.go`.

Hidden/shared paths:

- CLI `runPull` has richer behavior than `pkg/hfetch.Client.Pull`: vLLM profile,
  `--dest vllm`, interactive picker, flat collision checks, JSON progress, and
  serve-ready verification. This should become one library authority before the
  command is thinned.
- `ollama-import` is command-local and owns process execution, temp files, and
  Modelfile rendering today.

### Shared helpers and facades

- `internal/version`: build version surfaced by command roots.
- `internal/progress`: size/speed/bar formatting used by `llm-tidy` and suitable
  for command output consolidation.
- `internal/tui`: chat UI and status rendering used by `llm-run` and `llm-chat`.
- `internal/ui`: confirm/select helpers currently used by destructive flows.
- `pkg/hfetch`, `pkg/llmrun`, `pkg/llmserve`, `pkg/llmtidy`: existing public-ish
  facades that should become compatibility wrappers over `/internal/*` packages
  if the extraction keeps external import stability.
- `pkg/seam`: cross-package seam tests. These are not production code, but they
  document and protect current inter-package contracts.

## Current Domain Seams

| Seam | Current files/functions | Shared or command-specific | External side effects | Candidate package | Candidate internal API |
| --- | --- | --- | --- | --- | --- |
| Docker, Podman, Quadlet interactions | `pkg/llmserve/emit.Render`, `DockerRun`, `Compose`, `Quadlet`, `SpecHash`; `pkg/llmserve/runtime.Compose`; `pkg/llmserve/lifecycle.*`; `pkg/llmserve/liveness.*`; `cmd/llm-serve/*`; `pkg/llmtidy/interlock.LLMServeChecker` | Mostly shared under `pkg/llmserve`, with command-specific flag parsing and shell-out consumer in tidy | `docker compose up/down`, `docker ps`, `docker inspect`, persisted compose specs, watchdog script, liveness stdin/stdout contract | `internal/servespec`, `internal/servehost`, `internal/eviction` | `servespec.Render(target, contract, host)`, `servehost.Runtime`, `servehost.Prober`, `eviction.Liveness.FilterProtected(ctx, paths)` |
| Model discovery, registry, metadata | `pkg/hfetch/api`, `pkg/hfetch/registry`, `pkg/hfetch/source`, `pkg/hfetch/gguf`, `pkg/hfetch/quant`, `pkg/hfetch/fileset`; `pkg/llmrun/resolver`; `pkg/llmserve/artifact`; `pkg/llmtidy/inventory` | Shared, but `cmd/hfetch/pull.go` and `cmd/llm-run/models.go` still own important selection/scanning paths | HuggingFace HTTP, local registry manifest, local model files, GGUF reads, config/tokenizer reads | `internal/hub`, `internal/modelstore`, `internal/gguf`, `internal/modelmeta`, `internal/modelref` | `hub.Client.ListFiles/Search/GetModel`, `modelstore.Registry`, `modelmeta.DetectFacts/VerifyServeReady`, `modelref.Resolver.Resolve` |
| Runtime execution | `pkg/llmrun/engine.DetectBinaries`, `BuildCommand`, `Launch`, `WaitForReady`; `cmd/llm-run/runInference`, `serve`, `raw`; `pkg/llmserve/runtime.Compose`; `cmd/hfetch/ollama.go`; `pkg/llmtidy/interlock/shellout.go` | Shared low-level packages exist, high-level app orchestration remains command-specific | `exec.Command`, long-lived child processes, PID files, logs, signals, HTTP readiness, `ollama create`, `llm-serve liveness` shell-out | `internal/llamacpp`, `internal/inference`, `internal/process` | `llamacpp.Installation.Detect`, `llamacpp.Command.Build`, `inference.Run/Serve/Chat`, `process.Runner` |
| Config loading and defaults | `pkg/hfetch/config`, `cmd/hfetch/config.go`; `pkg/llmrun/config`, `pkg/llmrun/profiles`, `cmd/llm-run/profile.go`; `pkg/llmtidy/manifest/resolve.go`; `cmd/llm-serve/lifecycle.go:dirs`; `hardware.RecommendConfig`, command `applyDefaults` | Shared per tool, duplicated across tools | Environment reads, user home resolution, config/profile/token file reads and writes | `internal/paths`, `internal/hftoken`, `internal/runconfig`, `internal/tidymanifest` | `paths.Resolve(app)`, `hftoken.Resolve/Store/Clear`, `runconfig.Load/Save/Profiles`, `tidymanifest.Resolve/Load/Save` |
| Filesystem, state, cache paths | `pkg/hfetch/registry`, `pkg/hfetch/download`, `pkg/hfetch/api/cache.go`; `pkg/llmserve/instance.Store`, `EnsureWatchdogScript`, `cmd/llm-serve/dirs`; `pkg/llmrun/engine.Launch`, profiles and aliases; `pkg/llmtidy/manifest` | Shared state packages exist, path authority is split by tool | Registry writes, partial/state files, atomic renames, cache JSON, manifests, locks, PID/log files, watchdog scripts | `internal/paths`, `internal/modelstore`, `internal/download`, `internal/instances` | `paths.AppDirs`, `modelstore.Layout`, `download.Download`, `instances.Store` |
| HTTP/download/fetch behavior | `pkg/hfetch/api.Client`, `pkg/hfetch/source.File`, `pkg/hfetch/download.Download`; `pkg/llmtidy/ollama.Client`; `pkg/llmrun/api.Client`; `pkg/llmserve/runtime.HTTPProber`; command clients in `hfetch`, `llm-run`, `llm-serve` | Mostly shared, but client construction is command-local | HuggingFace REST, model file streams, range requests, retries/backoff, Ollama REST, llama/vLLM health and completions | `internal/hub`, `internal/download`, `internal/ollama`, `internal/openaiapi` | `hub.Client`, `download.FileSource`, `ollama.Client`, `openaiapi.Client`, injected `*http.Client` |
| Logging and output formatting | `cmd/*` renderers, `internal/progress`, `internal/tui`, `internal/ui`, `internal/tui/status.go`, `formatCrashLog`, status JSON structs | Mostly command-specific with small internal helpers | stdout/stderr, terminal styles, interactive prompts, TUI, JSON progress | `internal/output`, `internal/progress`, `internal/chatui` | `output.Writer`-based renderers, `progress.FormatSize/Bar`, `chatui.Run` |
| Process execution and shell boundaries | `pkg/llmrun/engine/detect.go`, `launch.go`, `hardware/detect.go`; `pkg/llmserve/runtime/compose.go`; `pkg/llmtidy/interlock/shellout.go`; `cmd/hfetch/ollama.go`; `cmd/llm-run/raw.go` | Mixed, with several direct command calls | `exec.LookPath`, `exec.Command`, `exec.CommandContext`, `syscall`, stdin/stdout/stderr forwarding | `internal/process`, plus domain packages that accept a runner | `process.Runner.LookPath/Run/Start`, domain methods taking `Runner` |

## Target Layout

Proposed first-pass layout. Names are domain-oriented and can be refined during
actual extraction.

```text
internal/
  paths/           XDG/env path resolution shared by all tools
  process/         command execution boundary and test runner adapters
  output/          non-interactive text/JSON render helpers
  progress/        existing size/speed/bar helpers
  hub/             HuggingFace Hub API and tree listing authority
  hftoken/         HuggingFace token resolution and persistence
  download/        resumable downloads, range fallback, rate limits
  modelstore/      local model registry, layout, GC, path lookup
  fileset/         vLLM/GGUF selection and serve-ready completeness gate
  gguf/            GGUF parsing, shard parsing, merge, quant filename parsing
  modelmeta/       quant metadata and serve artifact facts
  modelref/        model reference resolver and aliases
  ollama/          Ollama HTTP client and import Modelfile planning
  llamacpp/        llama.cpp binary detection, command build, process launch
  hardware/        CPU/GPU/NUMA detection and recommendation
  runconfig/       llm-run global config and profiles
  inference/       high-level chat/run/serve orchestration over llamacpp
  servecontract/   serving vocabulary, profiles, compat rules, contract resolve
  servespec/       compose, docker-run, and quadlet rendering
  serveinstance/   managed instance manifest store and lifecycle plan
  servehost/       Docker/Compose runtime and HTTP prober
  eviction/        llm-serve liveness and llm-tidy prune interlock contract
  tidymanifest/    llm-tidy manifest schema, resolve, load, validate, save
  inventory/       installed model inventory across Ollama, GGUF, vLLM
  reconcile/       desired-vs-installed diff, prune, sync planning
  chatui/          existing interactive chat TUI
  picker/          existing confirm/select UI
  version/         existing build version
```

Likely command shape after extraction:

- `cmd/hfetch`: Cobra flags, output, interactive picker; delegates to
  `hub`, `download`, `modelstore`, `fileset`, and `ollama`.
- `cmd/llm-run`: Cobra flags and output; delegates to `inference`, `modelref`,
  `runconfig`, `hardware`, and `llamacpp`.
- `cmd/llm-serve`: Cobra flags and output; delegates to `servecontract`,
  `servespec`, `serveinstance`, `servehost`, and `eviction`.
- `cmd/llm-tidy`: Cobra flags and output; delegates to `tidymanifest`,
  `inventory`, `reconcile`, and `eviction`.

## Risk-ranked Extraction Plan

1. Safest first: pure metadata and selection packages.
   - Start with `internal/modelmeta` for `pkg/hfetch/quant` plus relevant
     `pkg/llmserve/artifact` quant mapping.
   - Then `internal/fileset` and `internal/gguf`.
   - These have high current coverage and limited live side effects.

2. Next: state and path authorities.
   - Extract `internal/paths`, `internal/modelstore`, `internal/hftoken`, and
     `internal/tidymanifest`.
   - Keep compatibility wrappers temporarily in `pkg/*` so command behavior does
     not move and import churn stays mechanical.

3. Then: HuggingFace fetch/download.
   - Extract `internal/hub`, `internal/download`, and `internal/modelstore`.
   - Collapse `cmd/hfetch/runPull` and `pkg/hfetch.Client.Pull` into one pull
     authority before or during this phase.

4. Then: model reference and inventory.
   - Extract `internal/modelref`, `internal/inventory`, and `internal/reconcile`.
   - Keep the hfetch registry as the only local model store authority.

5. Then: serve contract/spec.
   - Extract `internal/servecontract` and `internal/servespec`.
   - This is mostly pure and already well tested.

6. Higher risk: managed serving host and eviction.
   - Extract `internal/serveinstance`, `internal/servehost`, and
     `internal/eviction`.
   - Preserve `llm-serve liveness --check` as the stable inter-tool contract
     during extraction.

7. Higher risk: llama.cpp runtime orchestration.
   - Extract `internal/llamacpp`, `internal/hardware`, `internal/runconfig`, and
     finally `internal/inference`.
   - `cmd/llm-run` has no direct tests today, so add seams before moving.

Riskiest package:

- `internal/servehost` if it includes the real Docker/Compose runtime plus
  liveness. It crosses process execution, labels, bind mounts, state recovery,
  and deletion protection. A wrong extraction can produce false "serving" or
  false "evictable" answers.

Close second:

- `internal/inference`, because `llm-run` currently mixes config resolution,
  hfetch registry lookup, hardware detection, llama.cpp binary probing, process
  launch, health polling, API calls, TUI startup, and signal handling in command
  files with zero direct command coverage.

Dependencies:

- `paths` is upstream of `hftoken`, `modelstore`, `runconfig`,
  `tidymanifest`, `serveinstance`, and `llamacpp` data/log paths.
- `hub` and `download` feed `modelstore`.
- `fileset`, `gguf`, and `modelmeta` feed `hfetch`, `llmserve/artifact`, and
  `llmrun/hardware` recommendations.
- `modelstore` feeds `modelref`, `inventory`, `hfetch`, and `llm-tidy`.
- `servecontract` feeds `servespec`; `servespec` feeds `serveinstance`;
  `servehost` feeds `serveinstance` and `eviction`.
- `eviction` feeds `llm-tidy` pruning.
- `llamacpp`, `runconfig`, `hardware`, and `modelref` feed `inference`.

Required tests before extraction:

- Add command-level tests for `cmd/llm-run` around `resolveModelArg`,
  `runInference` behavior with fake launcher/prober/API, `serve` flag override
  precedence, and `raw` binary selection.
- Add command-level tests for `cmd/llm-serve` around `emit`, `up` flag parsing,
  `dirs`, `liveness --check` stdin/stdout behavior, and `--target` rejection in
  `up`.
- Add parity tests proving `cmd/hfetch pull` and `pkg/hfetch.Client.Pull` use the
  same tree-listing authority, registry write shape, vLLM profile selection, and
  completeness behavior.
- Add tests for `cmd/hfetch ollama-import` planning with injected command runner
  before moving `ollama create` execution.
- Add tests that `llm-tidy` CLI pruning and `Tidy.Prune` use the same eviction
  authority, with no duplicated path-gating.
- Add path resolution tests for `llm-serve` state dirs and watchdog/spec paths.

Required tests after extraction:

- Keep old `pkg/*` wrapper tests until all callers are migrated.
- Add package-level tests for each new `internal/*` package matching the current
  behavior before deleting wrappers.
- Add command smoke tests that instantiate each root Cobra command and execute
  no-side-effect paths with temporary dirs and fake HTTP/process dependencies.
- Add integration tests behind build tags for real Docker/Compose and real
  llama.cpp only after the pure and fake-runtime tests pass.
- Re-run `go test ./...` and `go test -cover ./...` after every extraction
  phase, not only at the end.

## Phase 1 Extraction Log

Scope (operator-directed): only low-risk, already-tested, mostly-pure components.
Explicitly out of scope this phase: model metadata, hfetch pull, servehost,
Docker/Podman/Quadlet, llama.cpp launch, TTY, network-bound code. Rule honored:
where a duplicate authority was discovered, it was documented rather than merged.

### Moved

- **`internal/tidymanifest`** ← `pkg/llmtidy/manifest`. Single authority
  (llm-tidy-only consumers). Sources + full test suites relocated;
  `pkg/llmtidy/manifest` kept as an alias wrapper (types, consts, the
  `ErrNotFound` sentinel preserved by identity, funcs) so no consumer changed.
  Behavior preserved exactly; full suite green.

### Consolidated onto an existing authority

- **size formatting** — `cmd/hfetch.formatSize` was byte-identical to
  `internal/progress.FormatSize`; collapsed to delegate, removing a duplicate.
  No new `internal/output` package was created: the only shared output primitive
  (size/speed/bar) already lives in `internal/progress`, and the remaining
  command renderers (status JSON, prune/backend tables) are single-tool with no
  second consumer yet — extracting them now would be premature.

### Discovered duplicate authority — DOCUMENTED, NOT merged

- **`cmd/llm-run.formatSize` diverges.** It has **no KB tier** (sub-MB sizes
  render as bytes, e.g. `1500 B`), unlike `internal/progress.FormatSize`
  (`1.5 KB`). Merging it onto the shared authority would CHANGE `llm-run models`
  output, so it is left in place per the "preserve behavior exactly" rule. A
  regression test pins the current no-KB-tier behavior
  (`cmd/llm-run/models_test.go:TestFormatSize_NoKBTier`). Reconcile deliberately
  later: decide whether the KB tier is wanted for `llm-run`, then collapse.

### Deferred with reason (not extracted this phase)

- **`internal/paths`.** `pkg/hfetch/config.Dirs`, `pkg/llmrun/config.Dirs`, and
  `pkg/llmbench/config.Dirs` share a duplicated XDG mechanism (xdgConfig/Data/
  Cache + `<TOOL>_HOME` remap + per-dir overrides) but are **distinct per-tool
  authorities** differing in app name and env prefix; `cmd/llm-serve.dirs`
  (state-dir triad via `LLM_SERVE_HOME`/`XDG_STATE_HOME`) and
  `internal/tidymanifest.Resolve/ConfigDir` (config-only via `LLM_TIDY_*`) have
  DIFFERENT shapes that do not fit one `paths.For(app)` signature. A shared
  XDG-mechanism helper is worthwhile but is a multi-tool change with real
  behavior-preservation risk — out of scope for the "safest first" pass. Extract
  deliberately once a single parameterized shape (app name + env prefix +
  dir-set) is agreed, with per-tool behavior tests first.

### Confirmed already isolated

- **`internal/progress`** is already a properly isolated internal package
  (consumers: `cmd/llm-tidy`, `internal/ui`); 100% statement coverage. No move
  needed. It is the canonical size/speed/bar authority other tools should
  delegate to (llm-tidy and now hfetch do).

## Phase 2 Extraction Log

internal/paths — shared XDG/home MECHANISM extracted (no shared policy).

### Moved (mechanism)

- **`internal/paths`** now owns the duplicated XDG arithmetic: `Home()`
  (best-effort), `XDGConfig/XDGData/XDGCache/XDGState(app)`. These reproduce the
  exact prior behavior (env var wins; else home-joined canonical fallback; a ""
  home yields relative paths via filepath.Join).

### Delegated (policy stays tool-owned)

- `pkg/hfetch/config.Dirs` — keeps HFETCH_HOME remap + HFETCH_*_DIR overrides;
  XDG bases now call paths.XDG*. Local xdg* helpers deleted.
- `pkg/llmrun/config.Dirs` — keeps LLM_RUN_HOME remap + LLM_RUN_*_DIR overrides;
  XDG bases now call paths.XDG*. Local xdg* helpers deleted.
- `cmd/llm-serve.dirs` — keeps LLM_SERVE_HOME override + specs/watchdog layout;
  the state root now calls paths.XDGState("llm-serve").
- Each tool's existing dir tests stay green, pinning behavior.

### Path policies that remain tool-owned (NOT merged)

- App names, env-var prefixes, the override precedence order, and the directory
  SET each tool exposes (hfetch/llm-run: Config/Data/Cache triad; llm-serve:
  state root + specs + watchdog) are policy and stay in each package.

### Divergence intentionally preserved (NOT merged)

- **llm-tidy home-error policy.** `internal/tidymanifest.ConfigDir` PROPAGATES a
  home-resolution error ("cannot resolve user home directory"); hfetch/llm-run/
  llm-serve degrade best-effort to "". Per the hard rule, llm-tidy keeps its own
  ConfigDir/Resolve (it calls os.UserHomeDir directly, not paths.Home) so its
  observable behavior is unchanged. Reconcile later only if a global home-error
  policy is intentionally chosen.
- **llm-bench** (`pkg/llmbench/config.Dirs`) is out of the named phase-2 scope;
  it still has its own copy and can delegate to internal/paths in a later pass.

## Phase 3 Extraction Log

Pure-leaf domain tier (risk-plan phase 1). All safe relocations of self-contained,
well-tested packages; behavior preserved; pkg/* kept as alias wrappers.

### Moved
- **`internal/modelmeta`** ← `pkg/hfetch/quant` (ParseQuant/QuantInfo).
- **`internal/fileset`** ← `pkg/hfetch/fileset` (Verify completeness gate +
  SelectVLLM). Still imports `pkg/hfetch/api` (api not yet extracted).
- **`internal/gguf`** ← `pkg/hfetch/gguf` (parse/filter/fit/merge/shard,
  ParseQuantFromFilename, QuantBitsPerWeight). Self-contained, 11 consumers
  shielded by a full alias wrapper.

## Extraction Status (live)

Extracted to internal/ (with pkg/* compat wrappers, all green):
- Infra: `version`, `progress`, `tui`, `ui`, `paths` (mechanism), `tidymanifest`.
- Pure-leaf domain: `modelmeta`, `fileset`, `gguf`.

NOT yet extracted (remaining, dependency order) — see Risk-ranked plan above:
1. Config/pure tier: `hftoken` (hfetch token resolution), `hardware`,
   `runconfig` (llm-run config + profiles), `servecontract`, `servespec`.
2. State/registry: `modelstore` (hfetch/registry), `serveinstance`
   (llmserve/instance), `inventory`, `reconcile`.
3. Network/host-bound (ISOLATE BEHIND INJECTABLE INTERFACES FIRST): `hub`
   (hfetch/api), `download`, `ollama`, `openaiapi`, `llamacpp` (llmrun/engine),
   `servehost` (llmserve/runtime+lifecycle; already has Runtime/Prober ifaces),
   `eviction` (llmserve/liveness + llmtidy/interlock contract), `inference`
   (cmd/llm-run orchestration).

Duplicate authorities still to COLLAPSE (behavior-sensitive — not yet done):
- CLI `runPull` vs `pkg/hfetch.Client.Pull` (richer CLI behavior; blocked on a
  base-URL seam in `newAPIClient` — see seam audit). One pull authority wanted.
- CLI `runPrune` interlock vs `pkg/llmtidy.Tidy.Prune` (competing eviction
  path). One prune authority wanted.

## Accepted boundary: alias-wrapper runtime type identity

Compat wrappers re-export moved types via Go type ALIASES (`type X = internal.X`).
This is the idiomatic, zero-cost compat mechanism and is REQUIRED for values to
flow across the boundary unchanged. A consequence (raised by codex review of the
modelstore iteration, classified document-boundary): runtime type IDENTITY now
reports the internal/* authority — `%T`, `reflect.TypeOf().String()/PkgPath()` see
`modelstore.Registry`, not `registry.Registry`.

This is ACCEPTED, and verified inert: a repo-wide check found NO `%T` usage and NO
`reflect` type-name/PkgPath inspection of any wrapped type; encoding/json keys off
field tags (unchanged), not type names, so on-disk/wire formats are identical.
Applies uniformly to every alias wrapper (quant, fileset, gguf, manifest,
registry/modelstore, and future ones). Reversing it (defining types in pkg/* and
having internal/* alias upward) would defeat the one-authority goal, so it stays.
