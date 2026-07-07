# Spark Tools

A Go monorepo providing six tools for local LLM workflows on DGX Spark hardware:
- **hfetch** — HuggingFace Hub client (model discovery, download, GGUF metadata)
- **llm-run** — llama.cpp wrapper for GGUF (ergonomic inference, smart defaults, profiles)
- **llm-serve** — vLLM contract engine for safetensors (validated launch spec + compose lifecycle)
- **llm-chat** — Standalone chat TUI for any OpenAI-compatible endpoint
- **llm-bench** — Benchmark suite (declarative, automated, reproducible)
- **llm-tidy** — Model inventory management (desired-state manifest, prune/sync)

Serving routing rule: GGUF → llm-run, safetensors → llm-serve.

## Build & Test

All Go commands must use devbox:

```sh
devbox run build                       # Build all six binaries into the repo root
devbox run -- go build ./...           # Compile-check everything (no binaries)
devbox run -- go test ./...            # Run all tests
devbox run -- go build ./cmd/hfetch    # Build a single binary
devbox run -- go build ./cmd/llm-run
devbox run -- go build ./cmd/llm-serve
devbox run -- go build ./cmd/llm-chat
devbox run -- go build ./cmd/llm-bench
devbox run -- go build ./cmd/llm-tidy
```

## Repository Layout

```
cmd/hfetch/          CLI entrypoint for hfetch
cmd/llm-run/         CLI entrypoint for llm-run
cmd/llm-serve/       CLI entrypoint for llm-serve
cmd/llm-chat/        CLI entrypoint for llm-chat
cmd/llm-bench/       CLI entrypoint for llm-bench
cmd/llm-tidy/        CLI entrypoint for llm-tidy
pkg/hfetch/          hfetch library packages (api, auth, config, download, gguf, registry)
pkg/llmrun/          llm-run library packages (engine, resolver, profiles, hardware, api, config)
pkg/llmserve/        llm-serve library packages (runtime, lifecycle, servespec, contract)
pkg/llmbench/        llm-bench library packages (suite, job, metrics, prompts, report, store, syscheck)
pkg/llmtidy/         llm-tidy library packages (interlock, inventory, reconcile, ollama)
internal/            Shared internal packages (extraction targets: tui, modelstore, fileset, …)
specs/               Design specifications (read-only reference)
prompts/             Built-in benchmark prompt sets
```

## Conventions

- **Module path:** `github.com/lazypower/spark-tools`
- **Go version:** 1.25 (via devbox)
- **Zero cgo.** All packages must be pure Go.
- **Spec-driven:** Each tool has a spec in `specs/`. Read the spec before implementing.
- **Library-first:** CLIs are thin shells over `pkg/` packages. Business logic lives in packages.
- **Dependency order:** hfetch has no internal deps. llm-run depends on hfetch. llm-bench depends on both. llm-chat depends only on internal/tui and pkg/llmrun/api.
- **Auth is canonical in hfetch:** `pkg/hfetch/auth` defines sentinel errors. `pkg/hfetch/config` resolves tokens. Downstream tools import these — never duplicate auth logic.
- **XDG directories:** Each tool follows XDG Base Directory Specification with `TOOL_HOME` convenience overrides.
- **Terminal UI:** charmbracelet stack (huh for forms, bubbletea for interactive TUI, lipgloss for styling).
- **Error handling:** Typed errors with actionable user guidance. Auth errors pass through unchanged across tool boundaries.
- **Tests:** Unit tests alongside code. Integration tests use mock HTTP servers. No live API tests in CI.
