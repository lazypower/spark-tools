# Spark Tools

Pure Go toolchain for local LLM workflows on DGX Spark hardware. Four tools, zero cgo, one dependency chain.

```
hfetch ──▶ llm-run ──▶ llm-bench
 model       inference     benchmarks
 management  engine        & analysis
   │
   └──▶ llm-tidy
        inventory
        management
```

## hfetch

HuggingFace Hub client for GGUF *and* serve-ready safetensors workflows. Search, download, inspect, verify, and import models without touching Python.

```sh
hfetch search "qwen coder" --gguf          # Find GGUF models on the Hub
hfetch pull bartowski/Qwen2.5-Coder-32B-Instruct-GGUF --quant Q4_K_M
hfetch bartowski/Qwen2.5-Coder-32B-Instruct-GGUF      # Interactive picker
hfetch info nvidia/Qwen3.6-35B-A3B-NVFP4               # Shows quant format before you pull
hfetch ollama-import bartowski/Llama-3-8B-GGUF:Q4_K_M   # One-shot Ollama import
```

**Highlights:**
- GGUF-first file picker with quantization labels, bits-per-weight, and VRAM fit estimation
- **Serve-ready `vllm` profile** — pulls the complete safetensors fileset (all shards + configs + tokenizer + quant metadata + trust-remote-code `.py`) and refuses to leave a partial model on disk
- **Completeness gate** — every required file is verified present + size + hash (LFS SHA256 or git-blob SHA1); a missing or corrupt file is a named, non-zero-exit failure, never a silent partial
- **`hfetch verify`** — re-check a downloaded model against canonical HuggingFace hashes with no re-download (cron-able bitrot sweep via `--all`)
- **Quant-aware `info`** — reports NVFP4 / FP8 / GPTQ / compressed-tensors and KV-cache FP8 from the config, before you download
- Resumable parallel downloads (4 concurrent streams, HTTP Range)
- Automatic split-shard detection and pure Go GGUF merging
- Ollama import with template family auto-detection (Llama 3, ChatML, Phi-3, DeepSeek, Gemma, Mistral, and more)
- Local model registry with offline listing, path lookup, and garbage collection
- Token resolution: `--token` flag > `HFETCH_TOKEN` env > config file > HF CLI compat

**Serve-ready safetensors (vLLM):**

```sh
hfetch pull nvidia/Qwen3.6-35B-A3B-NVFP4 --dest vllm   # Complete set → flat ~/.../vllm/models/<name>, gate-checked
hfetch pull org/model --profile vllm --output ./models/m  # Same, into a dir you choose
hfetch verify org/model                                 # Re-hash a model vs upstream (no re-download)
hfetch verify --all                                     # Sweep every downloaded model for bitrot
```

A `--dest vllm` (or `--profile vllm`) pull lands files flat for vLLM to mount and exits non-zero naming any missing/corrupt file — the completeness gate is the `preflight_check` you don't have to write.

```sh
hfetch login                  # Store HF token (shared across all tools)
hfetch whoami                 # Check auth status
hfetch list                   # Show downloaded models
hfetch path org/model         # Print local path (for scripting)
hfetch files org/model        # List repo files with quant/size details
hfetch gc                     # Clean up partial downloads
```

## llm-run

Ergonomic llama.cpp wrapper. Auto-detects your hardware, picks smart defaults, and gets out of the way.

```sh
llm-run chat qwen-32b                     # Interactive chat TUI
llm-run chat qwen-32b --no-think          # Disable reasoning (--reasoning-budget 0)
llm-run chat qwen-32b --port 9090         # Custom port
llm-run chat qwen-32b --timeout 120       # Wait longer for big models to load
llm-run serve qwen-32b --parallel 4       # OpenAI-compatible API server
llm-run run qwen-32b --prompt "Explain NUMA in 3 sentences"  # Single shot
```

**Hardware-aware defaults** — detects your CPU, GPU, and memory, then sets threads, GPU layers, context size, batch size, flash attention, mmap, mlock, and NUMA strategy automatically. DGX Spark GB10 gets specific tuning (unified memory, full GPU offload, NUMA distribution).

```sh
llm-run hw                    # Show detected hardware + recommended config
llm-run hw --json             # Machine-readable output
```

**Profiles and aliases** — save configurations, name your models:

```sh
llm-run alias set qwen-32b bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M
llm-run profile save coding --model qwen-32b --ctx 32768 --system "You are a coding assistant"
llm-run chat --profile coding
```

**Model resolution** tries in order: aliases > hfetch registry > local paths > HF auto-pull.

```sh
llm-run models                # List available models (local + aliases)
llm-run explain context-size  # Learn what a parameter does
llm-run explain effective qwen-32b  # Show full computed config for a model
llm-run raw qwen-32b -- --flash-attn on --ctx-size 65536  # Escape hatch
```

**Chat TUI commands:** `/stats`, `/context`, `/system <prompt>`, `/temp <value>`, `/save <file>`, `/clear`, `/quit`

## llm-bench

Declarative benchmark suite. Define what you want to measure in YAML, get reproducible results.

```sh
llm-bench run bench.yaml                   # Run full suite
llm-bench run bench.yaml --dry-run         # Preview job matrix
llm-bench run bench.yaml --jobs "qwen*"    # Filter jobs by pattern
llm-bench quick qwen-32b                   # Ad-hoc single-model benchmark
llm-bench quick qwen-32b --ctx 4096,8192,16384  # Sweep context sizes
```

**Example config:**

```yaml
name: "Spark Throughput Sweep"

defaults:
  warmup_prompts: 3
  measure_prompts: 10
  max_tokens: 512
  temperature: 0.0
  cooldown_seconds: 10

models:
  - name: "Qwen2.5-Coder-32B"
    ref: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF"
    quants: ["Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0"]
    alias: "qwen-32b"

scenarios:
  - name: "throughput"
    context_sizes: [4096, 8192, 16384]
    prompts:
      builtin: "short"
    repeat: 3
```

This expands to a job matrix of models x quants x scenarios x repeats, runs each job with warmup, measurement, and cooldown, then stores structured results.

**Metrics collected:** prompt eval speed, generation speed (tok/s), time to first token, end-to-end latency, memory usage, CPU/GPU utilization. Aggregated as mean, median, p5, p95, stddev.

**Pre-flight checks** verify system idle state, thermal conditions, available memory, model availability, and llama.cpp binaries before burning hours on a run.

```sh
llm-bench results list                     # Browse stored runs
llm-bench results show run-20250312-143000 # View a specific run
llm-bench compare run-A run-B              # Side-by-side comparison
llm-bench report run-A --format markdown   # Export report
llm-bench prompts list                     # Available prompt sets
llm-bench init --hardware                  # Generate starter config from your hardware
```

Results are stored in `~/.local/share/llm-bench/results/` with full config snapshots for reproducibility.

## llm-tidy

Declarative model inventory management. Define which models should exist on this machine, prune the rest.

```sh
llm-tidy init                              # Bootstrap manifest from current inventory
llm-tidy status                            # Show blessed / untracked / missing
llm-tidy promote qwen2.5-coder:32b         # Add a model to the manifest
llm-tidy demote llama3:8b                  # Remove a model from the manifest
llm-tidy prune                             # Interactive removal of untracked models
llm-tidy prune --older-than 30d --yes      # Reclaim stale models, no prompt
llm-tidy sync                              # Pull manifest entries that are missing
```

**Why:** Ollama and hfetch make pulling models easy; nothing makes cleaning them up easy. A few weeks of experiments and the model store turns into a junk drawer with hundreds of GB locked up.

**Highlights:**
- Single YAML manifest declares the desired state across both backends (Ollama via REST, GGUF via hfetch registry)
- Tag normalization matches Ollama's `:latest` convention; case-insensitive repo matching for GGUF
- `--older-than 7d` protects experiments still in progress
- `--dry-run`, `--yes`, and `--backend` filters compose for safe automation
- `status --json` for scripting

**Example manifest** (`~/.config/llm-tidy/manifest.yaml`):

```yaml
version: 1

ollama:
  - name: qwen2.5-coder:32b
  - name: llama3.3:70b
  - name: nomic-embed-text:latest

gguf:
  - repo: unsloth/Qwen3.5-122B-A10B-GGUF
    quant: Q4_K_M
  - repo: mradermacher/Venus-120b-v1.0-i1-GGUF
```

A starter manifest lives at [`examples/llm-tidy/manifest.yaml`](examples/llm-tidy/manifest.yaml).

**Environment overrides:**

| Variable | Purpose |
|---|---|
| `LLM_TIDY_MANIFEST` | Explicit manifest path |
| `LLM_TIDY_CONFIG_DIR` | Config directory holding `manifest.yaml` |
| `OLLAMA_HOST` | Ollama server address (same variable Ollama itself reads) |

## Install

Requires Go 1.25+ (via [devbox](https://www.jetify.com/devbox)):

```sh
git clone https://github.com/lazypower/spark-tools.git
cd spark-tools
devbox run build    # Builds all four binaries
```

Or build individually:

```sh
devbox run -- go build ./cmd/hfetch
devbox run -- go build ./cmd/llm-run
devbox run -- go build ./cmd/llm-bench
devbox run -- go build ./cmd/llm-tidy
```

## Directory Layout

All tools follow the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/) with `*_HOME` convenience overrides.

| Tool | Config | Data | Cache |
|------|--------|------|-------|
| hfetch | `~/.config/hfetch/` | `~/.local/share/hfetch/` | `~/.cache/hfetch/` |
| llm-run | `~/.config/llm-run/` | `~/.local/share/llm-run/` | `~/.cache/llm-run/` |
| llm-bench | `~/.config/llm-bench/` | `~/.local/share/llm-bench/` | `~/.cache/llm-bench/` |
| llm-tidy | `~/.config/llm-tidy/` | (reuses hfetch data dir) | — |

## License

[MIT](LICENSE)
