# Spark Tools

A Go monorepo providing four tools for local LLM workflows on DGX Spark hardware:
- **hfetch** — HuggingFace Hub client (model discovery, download, GGUF metadata)
- **llm-run** — llama.cpp wrapper (ergonomic inference, smart defaults, profiles)
- **llm-bench** — Benchmark suite (declarative, automated, reproducible)
- **llm-chat** — Standalone chat TUI for any OpenAI-compatible endpoint

## Build & Test

All Go commands must use devbox:

```sh
devbox run -- go build ./...           # Build everything
devbox run -- go test ./...            # Run all tests
devbox run -- go build ./cmd/hfetch    # Build single binary
devbox run -- go build ./cmd/llm-run
devbox run -- go build ./cmd/llm-bench
devbox run -- go build ./cmd/llm-chat
```

## Repository Layout

```
cmd/hfetch/          CLI entrypoint for hfetch
cmd/llm-run/         CLI entrypoint for llm-run
cmd/llm-bench/       CLI entrypoint for llm-bench
cmd/llm-chat/        CLI entrypoint for llm-chat
pkg/hfetch/          hfetch library packages (api, auth, config, download, gguf, registry)
pkg/llmrun/          llm-run library packages (engine, resolver, profiles, hardware, api, config)
pkg/llmbench/        llm-bench library packages (suite, job, metrics, prompts, report, store, syscheck)
internal/            Shared internal packages (progress, ui, tui)
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
