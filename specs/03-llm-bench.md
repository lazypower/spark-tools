# llm-bench — LLM Benchmark Suite

**Status:** Draft
**Created:** 2026-02-27
**Component:** `llm-bench`
**Language:** Go
**Depends on:** `hfetch` (model resolution, metadata), `llm-run` (inference engine management)

---

## 1. Problem Statement

Benchmarking local LLMs is tedious and error-prone. Every time you want to answer "which quantization is best for my use case?" or "how does this 70B model compare to that 32B model on my hardware?", you end up:

1. Manually constructing `llama-bench` or `llama-server` commands.
2. Carefully recording parameters in a spreadsheet.
3. Running prompts by hand, copy-pasting timing data.
4. Trying to remember what you changed between runs.
5. Never quite trusting the numbers because you forgot whether the system was idle.

A DGX Spark on the desk makes this problem worse, not better — now there are *more* models worth testing, *more* quantizations to compare, and *more* configurations to explore. The surface area of interesting benchmarks explodes.

`llm-bench` makes benchmarking declarative. Define what you want to measure in a YAML config, point it at models, and walk away. It handles the mechanics: launching servers, warming up, running prompts, collecting metrics, cooling down, and producing structured results. The human sets the parameters and reads the report. The machine does the lifting.

## 2. Goals

1. **Declarative benchmark definitions.** YAML configs define what to benchmark. No imperative scripting.
2. **Full automation.** From model loading to result collection, no human intervention required during a run.
3. **Repeatable.** Same config produces comparable results. Control for warmup, system load, and variance.
4. **Multi-dimensional comparison.** Compare across models, quantizations, context lengths, batch sizes, and hardware configurations in a single run.
5. **Structured output.** Results in JSON/CSV for analysis. Human-readable summaries for quick review.
6. **Safe for unattended operation.** Handles errors, timeouts, and resource exhaustion without crashing or leaving orphan processes.

## 3. Non-Goals

- Evaluating model *quality* (accuracy, reasoning, coding ability). This is a performance benchmark tool, not an eval framework.
- Real-time monitoring dashboards. Results are batch-collected and reported.
- Distributed benchmarking across multiple machines.
- Benchmarking non-llama.cpp backends.

## 4. Architecture

### 4.1 Package Structure

```
llm-bench/
├── cmd/llm-bench/             # CLI entrypoint
├── pkg/
│   ├── suite/                 # Benchmark suite orchestration
│   │   ├── runner.go          # Top-level run loop
│   │   ├── scheduler.go       # Sequences benchmark jobs, manages cooldown
│   │   └── config.go          # Parse and validate benchmark YAML configs
│   ├── job/                   # Individual benchmark job execution
│   │   ├── job.go             # Single benchmark job (one model, one config)
│   │   ├── warmup.go          # Warmup phase logic
│   │   ├── probe.go           # Send prompts and collect metrics
│   │   └── cooldown.go        # Post-job cleanup and stabilization
│   ├── metrics/               # Metrics collection and aggregation
│   │   ├── collector.go       # Collect raw timing data from API responses
│   │   ├── aggregator.go      # Compute statistics (mean, median, p95, stddev)
│   │   ├── system.go          # System-level metrics (CPU, memory, GPU utilization)
│   │   └── types.go           # Metric types and units
│   ├── prompts/               # Prompt management
│   │   ├── builtin.go         # Built-in benchmark prompts
│   │   ├── loader.go          # Load custom prompts from files
│   │   └── sizing.go          # Prompt sizing via server tokenization or byte-length fallback
│   ├── report/                # Report generation
│   │   ├── json.go            # JSON output
│   │   ├── csv.go             # CSV output
│   │   ├── terminal.go        # Terminal-formatted summary tables
│   │   └── compare.go         # Cross-run comparison reports
│   ├── store/                 # Result storage
│   │   ├── store.go           # Persist benchmark results
│   │   └── query.go           # Query historical results
│   └── syscheck/              # Pre-flight system checks
│       ├── idle.go            # Check system is reasonably idle
│       ├── thermal.go         # Check for thermal throttling
│       └── resources.go       # Verify sufficient memory/disk
└── prompts/                   # Built-in prompt sets
    ├── short.txt              # Short prompts (~50 tokens)
    ├── medium.txt             # Medium prompts (~500 tokens)
    ├── long.txt               # Long prompts (~2000 tokens)
    ├── code.txt               # Code-specific prompts
    └── reasoning.txt          # Multi-step reasoning prompts
```

### 4.2 Terminal UI Stack

llm-bench has no interactive UI — it runs unattended by design. Terminal output uses:

- **[`charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss)** — styling for result tables, comparison matrices, progress indicators. Shared across the toolchain for visual consistency with hfetch and llm-run.

No bubbletea, no huh. Zero interactivity during benchmark runs.

**Pre-run confirmation:** if TTY is attached and `--yes` is not set, show the job matrix summary and wait for Enter. If no TTY (CI, cron, scripts), skip the prompt entirely — never block on stdin in non-interactive mode.

### 4.3 Core Types

```go
// BenchmarkSuite is the top-level config defining a complete benchmark run
type BenchmarkSuite struct {
    Name        string              `yaml:"name"`
    Description string              `yaml:"description"`
    Defaults    JobDefaults         `yaml:"defaults"`
    Models      []ModelSpec         `yaml:"models"`
    Scenarios   []Scenario          `yaml:"scenarios"`
    Settings    SuiteSettings       `yaml:"settings"`
}

// JobDefaults provides default values inherited by all jobs
type JobDefaults struct {
    WarmupPrompts   int             `yaml:"warmup_prompts"`
    MeasurePrompts  int             `yaml:"measure_prompts"`
    MaxTokens       int             `yaml:"max_tokens"`
    Temperature     float64         `yaml:"temperature"`
    CooldownSeconds int             `yaml:"cooldown_seconds"`
    Timeout         Duration        `yaml:"timeout"`
}

// ModelSpec defines a model to benchmark
type ModelSpec struct {
    Name    string                  `yaml:"name"`
    Ref     string                  `yaml:"ref"`       // hfetch model reference
    Quants  []string                `yaml:"quants"`    // quantization variants to test
    Alias   string                  `yaml:"alias"`     // Short name for reports
}

// Scenario defines a benchmark scenario (set of conditions to test)
type Scenario struct {
    Name          string            `yaml:"name"`
    Description   string            `yaml:"description"`
    ContextSizes  []int             `yaml:"context_sizes"`
    BatchSizes    []int             `yaml:"batch_sizes"`
    ParallelSlots []int             `yaml:"parallel_slots"`
    Prompts       PromptSet         `yaml:"prompts"`
    MaxTokens     int               `yaml:"max_tokens"`
    Repeat        int               `yaml:"repeat"`     // Run each config N times
}

// PromptSet defines which prompts to use
type PromptSet struct {
    Builtin  string                 `yaml:"builtin"`    // "short", "medium", "long", "code"
    File     string                 `yaml:"file"`       // Custom prompt file
    Inline   []string               `yaml:"inline"`     // Inline prompt strings
}

// SuiteSettings controls overall benchmark behavior
type SuiteSettings struct {
    OutputDir        string         `yaml:"output_dir"`
    OutputFormats    []string       `yaml:"output_formats"` // "json", "csv", "terminal"
    AbortOnError     bool           `yaml:"abort_on_error"`
    SystemCheck      bool           `yaml:"system_check"`     // Pre-flight system checks
    DirtyMode        string         `yaml:"dirty_mode"`        // "abort" (default), "warn", "force"
    CooldownBetween  int            `yaml:"cooldown_between"`  // Seconds between jobs
    ServerStartupTimeout Duration   `yaml:"server_startup_timeout"`
    MetricsSampleMs  int            `yaml:"metrics_sample_ms"` // System metrics interval (default: 500)
}

// BenchmarkResult is the output of a single benchmark job.
// Must capture enough context for full reproducibility — someone looking
// at this result months later should know exactly what ran and how.
type BenchmarkResult struct {
    SchemaVersion int                  `json:"schema_version"` // Currently 1

    // Identity
    JobID         string               // e.g. "qwen-32b-Q4_K_M-throughput-1"
    ScenarioName  string
    ScenarioID    string               // Content-hash of scenario config + prompt set (see 5.4)
    RunIndex      int

    // Provenance — from llm-run's ResolvedModel
    Model         llmrun.ResolvedModel // Path, source, normalized ref, quant, GGUF metadata, WasPulled

    // Effective launch config — the actual RunConfig after defaults/scenario/profile merging
    EffectiveConfig llmrun.RunConfig

    // llama.cpp details — from llm-run's Capabilities + BuildCommand
    Capabilities  llmrun.Capabilities  // Backend, features, version
    EffectiveFlags []string             // Exact flags passed to llama-server
    FlagWarnings  []string              // Degraded features from BuildCommand

    // Timing
    ModelLoadTime   Duration
    FirstTokenTime  ThroughputStats  // Time to first token (TTFT) distribution

    // Throughput
    PromptEval      ThroughputStats  // Tokens/sec for prompt processing
    Generation      ThroughputStats  // Tokens/sec for generation
    EndToEnd        ThroughputStats  // Total request latency

    // Resource usage
    SystemMetrics   *SystemMetrics   // nil if collection unavailable (see 6.4)

    // Metadata
    Hardware      llmrun.HardwareInfo
    Timestamp     time.Time
    Duration      Duration
}

// SystemMetrics captures resource utilization during the measurement phase.
// Optional — populated on best-effort basis depending on platform support.
type SystemMetrics struct {
    Available        bool     // Whether system metrics were successfully collected
    PeakMemoryMB     int64
    PeakGPUMemoryMB  int64
    MeanCPUPercent   float64
    MeanGPUPercent   float64
    PeakGPUPercent   float64
    ThermalThrottled bool     // True if throttling was detected during measurement
    SampleCount      int      // Number of samples taken
    SampleIntervalMs int      // Sampling interval used
}

// ThroughputStats holds aggregated performance metrics
type ThroughputStats struct {
    Mean     float64
    Median   float64
    P5       float64
    P95      float64
    StdDev   float64
    Min      float64
    Max      float64
    Samples  int
}
```

## 5. Benchmark Configuration

### 5.1 Example Config

```yaml
name: "DGX Spark Model Comparison"
description: "Compare 32B and 70B models across quantizations on DGX Spark GB10"

defaults:
  warmup_prompts: 3
  measure_prompts: 10
  max_tokens: 512
  temperature: 0.0          # Deterministic for consistency
  cooldown_seconds: 10
  timeout: 5m

models:
  - name: "Qwen2.5-Coder-32B"
    ref: "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF"
    quants: ["Q4_K_M", "Q5_K_M", "Q6_K", "Q8_0"]
    alias: "qwen-32b"

  - name: "Qwen2.5-72B"
    ref: "bartowski/Qwen2.5-72B-Instruct-GGUF"
    quants: ["Q4_K_M", "Q5_K_M"]
    alias: "qwen-72b"

  - name: "DeepSeek-R1-0528-120B"
    ref: "unsloth/DeepSeek-R1-0528-GGUF"
    quants: ["Q4_K_M"]
    alias: "deepseek-r1"

scenarios:
  - name: "throughput"
    description: "Raw generation throughput"
    context_sizes: [4096]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "short"
    max_tokens: 512
    repeat: 3

  - name: "context-scaling"
    description: "Performance across context sizes"
    context_sizes: [4096, 8192, 16384, 32768]
    batch_sizes: [512]
    parallel_slots: [1]
    prompts:
      builtin: "medium"
    max_tokens: 256
    repeat: 2

  - name: "concurrency"
    description: "Performance under parallel load"
    context_sizes: [4096]
    batch_sizes: [512]
    parallel_slots: [1, 2, 4]
    prompts:
      builtin: "short"
    max_tokens: 256
    repeat: 3

settings:
  output_dir: "./results"
  output_formats: ["json", "terminal"]
  system_check: true
  cooldown_between: 15
  server_startup_timeout: 2m
```

### 5.2 Job Matrix

The suite expands configs into a job matrix. For the above config:

```
Models(3) × Quants(varies) × Scenarios(3) × Context/Batch/Parallel combos × Repeat

Example expansion for "throughput" scenario:
  qwen-32b   Q4_K_M  ctx=4096 batch=512 parallel=1  run 1/3
  qwen-32b   Q4_K_M  ctx=4096 batch=512 parallel=1  run 2/3
  qwen-32b   Q4_K_M  ctx=4096 batch=512 parallel=1  run 3/3
  qwen-32b   Q5_K_M  ctx=4096 batch=512 parallel=1  run 1/3
  ... (all quant variants)
  qwen-72b   Q4_K_M  ctx=4096 batch=512 parallel=1  run 1/3
  ...
  deepseek-r1 Q4_K_M ctx=4096 batch=512 parallel=1  run 1/3
  ...
```

Total jobs are displayed before the run starts, with estimated total time based on historical data.

### 5.3 Job Ordering

Jobs are ordered to minimize model load/unload cycles:

1. Group by model (avoid reloading the same model).
2. Within a model, group by quantization.
3. Within a quantization, run scenarios in definition order.
4. Within a scenario, iterate config combinations, then repeats.

### 5.4 Scenario Identity

Each scenario gets a stable `scenario_id`: a content-hash (SHA-256, truncated to 12 hex chars) computed from:
- The scenario's YAML config fields (name, context_sizes, batch_sizes, parallel_slots, max_tokens, repeat)
- The prompt set content (actual prompt text, not just the set name)

This ensures "throughput" means the same thing across runs. If you change the prompt set or tweak a parameter, the scenario_id changes and comparisons are flagged as non-equivalent.

The scenario `name` field remains the human-readable label. The `scenario_id` is the machine-comparable fingerprint.

## 6. Execution Pipeline

### 6.1 Per-Job Pipeline

Each benchmark job follows a strict pipeline:

```
┌─────────────────────────────────────────────────────┐
│ 1. PRE-FLIGHT                                       │
│    ├── Verify model file exists (pull if needed*)     │
│    ├── System idle check (CPU < 15%, no throttling)  │
│    └── Verify sufficient free memory                 │
├─────────────────────────────────────────────────────┤
│ 2. SERVER LAUNCH                                    │
│    ├── Build RunConfig from job spec                 │
│    ├── Launch llama-server via llm-run engine        │
│    ├── Wait for /health to return "ok"               │
│    └── Record model load time                        │
├─────────────────────────────────────────────────────┤
│ 3. WARMUP                                           │
│    ├── Send N warmup prompts (results discarded)     │
│    └── Wait for stable state                         │
├─────────────────────────────────────────────────────┤
│ 4. MEASURE                                          │
│    ├── Send prompts sequentially (or in parallel)    │
│    ├── For each prompt:                              │
│    │   ├── Record time-to-first-token (TTFT)         │
│    │   ├── Record per-token timing                   │
│    │   ├── Record total generation time              │
│    │   └── Record prompt eval time                   │
│    └── Collect system metrics during generation      │
├─────────────────────────────────────────────────────┤
│ 5. TEARDOWN                                         │
│    ├── Gracefully stop the server                    │
│    ├── Record peak memory usage                      │
│    └── Cooldown period before next job               │
└─────────────────────────────────────────────────────┘
```

**\*Model pulling is deterministic.** Every job in a benchmark suite resolves to `model:quant` (the YAML `ref` + `quants` fields always produce an explicit quantization). llm-bench never opens an interactive picker. If a model is missing locally:
- Pull it via hfetch with the exact quant specified — no ambiguity.
- If auth is required and no token is available, fail with `auth.ErrAuthRequired`, record the failure, continue to next job (unless `abort_on_error`).
- Auth errors are never reworded — the user sees hfetch's canonical guidance.

### 6.2 Metric Collection

Metrics are collected from multiple sources:

**From llama-server API responses:**
- `timings.prompt_n` — tokens in prompt
- `timings.prompt_ms` — prompt eval time
- `timings.predicted_n` — tokens generated
- `timings.predicted_ms` — generation time

The API returns these in the `/v1/chat/completions` response (and in the `usage` field for OpenAI-compatible responses). We also use `/slots` and `/metrics` endpoints for detailed per-slot data.

**From system probes (sampled during measurement phase — see section 6.4):**
- CPU utilization (from `/proc/stat` on Linux, `host_processor_info` on macOS)
- Memory usage (from `/proc/meminfo` on Linux, `host_statistics` on macOS)
- GPU utilization and memory (from `nvidia-smi` CLI — optional external dependency, see 6.4)
- Thermal state (from `nvidia-smi` on NVIDIA GPUs)

**Computed from raw data:**
- Tokens/second (prompt eval): `prompt_n / (prompt_ms / 1000)`
- Tokens/second (generation): `predicted_n / (predicted_ms / 1000)`
- Time-to-first-token (TTFT): time from request sent to first SSE token received
- End-to-end latency: time from request sent to final token received

### 6.3 Parallel Load Testing

For concurrency scenarios (`parallel_slots: N` where N > 1):

**Server and client must match.** The `parallel_slots` value in the YAML controls both sides:
- llm-run launches llama-server with `--parallel N` (server allocates N context slots).
- llm-bench launches exactly N client goroutines sending prompts concurrently.

This ensures the benchmark measures actual parallel inference, not server-side request queueing.

**Execution:**
- Stagger goroutine start times by 100ms to avoid thundering herd on the first request.
- Each goroutine sends its share of prompts sequentially (total prompts divided evenly across goroutines).
- Each goroutine collects per-request metrics independently.

**Metrics for parallel scenarios must distinguish:**

| Metric | Meaning |
|---|---|
| Per-request TTFT | Distribution of time-to-first-token across individual requests |
| Per-request generation tok/s | How fast each individual request generates |
| Aggregate throughput tok/s | Sum of all concurrent generation streams — system-level throughput |
| Queue/reject count | Requests that were queued or rejected by the server (if any) |

Per-request metrics use the same `ThroughputStats` aggregation. Aggregate throughput is reported as a separate field in the result.

### 6.4 System Metrics Collection

System-level metrics (CPU, memory, GPU) are collected on a **best-effort** basis. This is a pure-Go tool — we avoid cgo and NVML bindings.

**Collection backend:**

| Metric | Linux | macOS | Source |
|---|---|---|---|
| CPU utilization | `/proc/stat` | `host_processor_info` (syscall) | Pure Go, no deps |
| Memory usage | `/proc/meminfo` | `host_statistics` (syscall) | Pure Go, no deps |
| GPU utilization | `nvidia-smi --query-gpu=...` | N/A | External binary, optional |
| GPU memory | `nvidia-smi --query-gpu=...` | N/A | External binary, optional |
| GPU temperature | `nvidia-smi --query-gpu=...` | N/A | External binary, optional |

`nvidia-smi` is an **optional external dependency**, not a build requirement. If it's not on `$PATH`, GPU metrics are simply not collected. Results record `system_metrics.available: true/false` so downstream consumers know whether GPU numbers are present.

For DGX Spark specifically: `nvidia-smi` ships with the NVIDIA drivers and should always be available.

**Sampling contract:**

- Sampling **starts at the beginning of the measurement phase** (after warmup, not during server launch).
- Sampling **stops at the end of the measurement phase** (before teardown).
- Default sample interval: **500ms** (configurable in suite settings).
- Peak values are computed over measurement-phase samples only (warmup excluded).
- Results record `sample_count` and `sample_interval_ms` so consumers can assess data quality.
- If fewer than 3 samples were collected (very short benchmark), `system_metrics.available` is set to `false` with a warning — the data is too sparse to be meaningful.

## 7. Pre-Flight System Checks

Before starting a benchmark run, verify the system is in a suitable state:

### 7.1 Idle Check

```go
type IdleCheck struct {
    MaxCPUPercent    float64  // Abort if system CPU > threshold (default: 15%)
    MaxGPUPercent    float64  // Abort if GPU utilization > threshold (default: 10%)
    SampleDuration   Duration // How long to sample (default: 5s)
    WarnOnly         bool     // Warn instead of abort
}
```

### 7.2 Thermal Check

- Read CPU/GPU temperature where platform APIs allow.
- Warn if temperature suggests thermal throttling is likely.
- On DGX Spark: check via NVIDIA SMI for GPU thermal state.

### 7.3 Resource Check

- Verify free memory exceeds estimated model size + KV cache + headroom.
- Verify free disk space for result storage.
- Check that required llama.cpp binaries are available.
- Verify all referenced models are available locally (or can be pulled).
- Verify HuggingFace authentication via `hfetch/pkg/config.ResolveToken("")` if any models may need to be pulled (especially gated models). Auth is provided canonically by hfetch — `hfetch login` once authenticates the entire toolchain. Surfaces `auth.ErrAuthRequired` with consistent "Run `hfetch login`" guidance.

### 7.4 Warnings vs. Abort

Each check can either warn (log and continue) or abort (stop the run). The `dirty_mode` setting controls the threshold globally:

| `dirty_mode` | Behavior |
|---|---|
| `abort` (default) | Abort on resource issues (memory, disk, missing binaries). Warn on non-ideal conditions (CPU not fully idle, mild thermal). |
| `warn` | Warn on everything, abort on nothing. Run "good enough" benchmarks without editing per-check settings. Results are tagged with warnings for transparency. |
| `force` | No checks at all. For debugging or quick iteration. Results are tagged `"dirty_mode": "force"`. |

```
$ llm-bench run comparison.yaml

  Pre-flight checks:
    ✓ llama.cpp found (version b5432)
    ✓ All models available locally
    ✓ Sufficient memory (128 GB available, max job needs ~45 GB)
    ✓ Sufficient disk space (12 GB free for results)
    ⚠ CPU utilization at 8.2% (threshold: 15%) — acceptable
    ✓ GPU idle (0.1% utilization)
    ✓ No thermal throttling detected

  Ready to run 42 benchmark jobs (estimated time: ~35 minutes)
  Press Enter to start...
```

## 8. CLI Interface

### 8.1 Commands

```
llm-bench run <config.yaml>         Run a benchmark suite
  --dry-run                          Show job matrix without executing
  --yes                              Skip "Press Enter to start" confirmation
  --jobs <pattern>                   Filter jobs by name/model pattern
  --output-dir <dir>                 Override output directory
  --skip-check                       Skip pre-flight system checks
  --dirty-mode <mode>                Override dirty_mode (abort/warn/force)
  --continue-from <job_id>           Resume a failed/interrupted run

llm-bench quick <model>             Quick single-model benchmark
  --quant <type>                     Quantization to test
  --ctx <sizes>                      Context sizes (comma-separated)
  --prompts <n>                      Number of measurement prompts (default: 10)

llm-bench results list              List stored benchmark results
  --model <pattern>                  Filter by model
  --after <date>                     Results after date
  --before <date>                    Results before date

llm-bench results show <run_id>     Show results from a specific run
  --format <fmt>                     Output format: terminal, json, csv

llm-bench compare <run_id...>       Compare results across runs
  --metric <name>                    Focus on specific metric
  --format <fmt>                     Output format

llm-bench report <run_id>           Generate a formatted report
  --format <fmt>                     terminal, json, csv, markdown
  --output <file>                    Write to file instead of stdout

llm-bench prompts list              List available prompt sets
llm-bench prompts show <name>       Preview prompts in a set

llm-bench init                      Generate a starter benchmark config
  --models <refs>                    Pre-populate with model references
  --hardware                         Auto-detect hardware and suggest scenarios
```

### 8.2 Quick Benchmark

For fast, ad-hoc benchmarking without writing a YAML config:

```
$ llm-bench quick qwen-32b

  Quick Benchmark: Qwen2.5-Coder-32B-Instruct (Q4_K_M)
  Hardware: DGX Spark GB10 (128 GB)
  ══════════════════════════════════════════════════════

  Loading model... done (4.2s)
  Warmup (3 prompts)... done
  Measuring (10 prompts)...

  Results:
  ┌──────────────────────────────────────────────────────┐
  │ Prompt Processing   │ 1,842.3 tok/s (±23.1)         │
  │ Generation           │ 48.7 tok/s (±1.2)            │
  │ Time to First Token  │ 127ms median (p95: 203ms)    │
  │ Model Load Time      │ 4.2s                         │
  │ Peak Memory          │ 22.4 GB                      │
  │ Context Size         │ 4,096 tokens                 │
  └──────────────────────────────────────────────────────┘
```

### 8.3 Comparison Output

```
$ llm-bench compare run-20260227-001 run-20260227-002

  Comparison: "throughput" scenario
  ══════════════════════════════════════════════════════════════════

  Generation Speed (tok/s)
  ┌────────────────────────┬────────┬────────┬────────┬──────────┐
  │ Model                  │ Q4_K_M │ Q5_K_M │ Q6_K   │ Q8_0     │
  ├────────────────────────┼────────┼────────┼────────┼──────────┤
  │ Qwen2.5-Coder-32B      │ 48.7   │ 42.1   │ 36.8   │ 28.3     │
  │ Qwen2.5-72B             │ 22.4   │ 18.9   │  —     │  —       │
  │ DeepSeek-R1-120B        │ 14.2   │  —     │  —     │  —       │
  └────────────────────────┴────────┴────────┴────────┴──────────┘

  Time to First Token (ms, median)
  ┌────────────────────────┬────────┬────────┬────────┬──────────┐
  │ Model                  │ Q4_K_M │ Q5_K_M │ Q6_K   │ Q8_0     │
  ├────────────────────────┼────────┼────────┼────────┼──────────┤
  │ Qwen2.5-Coder-32B      │ 127    │ 134    │ 142    │ 163      │
  │ Qwen2.5-72B             │ 312    │ 338    │  —     │  —       │
  │ DeepSeek-R1-120B        │ 587    │  —     │  —     │  —       │
  └────────────────────────┴────────┴────────┴────────┴──────────┘

  Peak Memory (GB)
  ┌────────────────────────┬────────┬────────┬────────┬──────────┐
  │ Model                  │ Q4_K_M │ Q5_K_M │ Q6_K   │ Q8_0     │
  ├────────────────────────┼────────┼────────┼────────┼──────────┤
  │ Qwen2.5-Coder-32B      │ 22.4   │ 25.8   │ 29.1   │ 36.7     │
  │ Qwen2.5-72B             │ 44.1   │ 51.2   │  —     │  —       │
  │ DeepSeek-R1-120B        │ 73.8   │  —     │  —     │  —       │
  └────────────────────────┴────────┴────────┴────────┴──────────┘
```

## 9. Result Storage

### 9.1 Storage Layout

Results live in `$LLM_BENCH_DATA_DIR/results/`:

```
$LLM_BENCH_DATA_DIR/results/
└── <run_id>/                           # e.g. run-20260227-233000
    ├── config.yaml                     # Copy of the input config
    ├── results.json                    # Full structured results (schema_version: 1)
    ├── summary.json                    # Aggregated summary
    ├── system.json                     # Hardware and system info snapshot
    └── jobs/
        └── <job_id>.json              # Per-job raw data (schema_version: 1)
```

### 9.2 Result Identity

Each run gets a unique ID: `run-YYYYMMDD-HHMMSS`. Each job within a run gets an ID: `<model_alias>-<quant>-<scenario>-<run_index>`.

### 9.3 Result Schema

Results are stored as structured JSON for downstream analysis:

```json
{
  "schema_version": 1,
  "run_id": "run-20260227-233000",
  "suite_name": "DGX Spark Model Comparison",
  "dirty_mode": "abort",
  "preflight_warnings": [],
  "started_at": "2026-02-27T23:30:00Z",
  "completed_at": "2026-02-27T23:58:42Z",
  "hardware": {
    "cpu": "NVIDIA Grace (12 cores)",
    "cpu_cores": 12,
    "memory_gb": 128,
    "gpu": "NVIDIA GB10",
    "gpu_memory_gb": 128,
    "is_dgx_spark": true
  },
  "jobs": [
    {
      "schema_version": 1,
      "job_id": "qwen-32b-Q4_K_M-throughput-1",
      "scenario_name": "throughput",
      "scenario_id": "a3f8c2...",
      "run_index": 1,

      "model": {
        "path": "/home/chuck/.local/share/hfetch/models/bartowski--Qwen2.5-Coder-32B-Instruct-GGUF/Qwen2.5-Coder-32B-Instruct-Q4_K_M.gguf",
        "source": "registry",
        "requested_ref": "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M",
        "normalized_ref": "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF:Q4_K_M",
        "quant": "Q4_K_M",
        "registry_id": "bartowski/Qwen2.5-Coder-32B-Instruct-GGUF",
        "was_pulled": false,
        "gguf_meta": {
          "architecture": "qwen2",
          "parameter_count": 32000000000,
          "context_length": 32768,
          "quant_type": "Q4_K_M"
        }
      },

      "capabilities": {
        "version": "b5432",
        "backend": "cuda",
        "cuda_compute": "sm_100",
        "flash_attention": true,
        "numa": true
      },

      "effective_config": {
        "context_size": 4096,
        "batch_size": 512,
        "parallel_slots": 1,
        "max_tokens": 512,
        "temperature": 0.0,
        "flash_attention": true,
        "gpu_layers": -1,
        "numa_strategy": "distribute"
      },

      "effective_flags": ["--model", "/path/to/model.gguf", "--ctx-size", "4096", "--batch-size", "512", "--flash-attn", "--numa", "distribute", "--gpu-layers", "-1"],
      "flag_warnings": [],

      "status": "ok",
      "error": null,

      "results": {
        "model_load_time_ms": 4200,
        "prompt_eval_tok_s": {
          "mean": 1842.3, "median": 1838.0, "p5": 1801.2, "p95": 1889.4,
          "stddev": 23.1, "min": 1790.0, "max": 1901.2, "samples": 10
        },
        "generation_tok_s": {
          "mean": 48.7, "median": 48.9, "p5": 46.2, "p95": 50.1,
          "stddev": 1.2, "min": 45.8, "max": 51.3, "samples": 10
        },
        "ttft_ms": {
          "mean": 131.2, "median": 127.0, "p5": 112.0, "p95": 203.0,
          "stddev": 28.4, "min": 108.0, "max": 215.0, "samples": 10
        },
        "system_metrics": {
          "available": true,
          "peak_memory_mb": 22938,
          "peak_gpu_memory_mb": 21504,
          "mean_cpu_pct": 12.4,
          "mean_gpu_pct": 94.2,
          "peak_gpu_pct": 98.1,
          "thermal_throttled": false,
          "sample_count": 47,
          "sample_interval_ms": 500
        }
      }
    }
  ]
}
```

## 10. Built-in Prompt Sets

### 10.1 Design Principles

- Each prompt set targets a specific input length range.
- Prompts are diverse enough to avoid caching artifacts.
- Prompts request specific output lengths where possible (e.g., "respond in exactly 3 sentences").
- Temperature is set to 0.0 for deterministic output (consistent token counts across runs).

### 10.2 Prompt Sizing Strategy

Prompt sets describe length ranges, but "~50 tokens" is model-dependent. To avoid footguns:

**Preferred: server-side tokenization.** During warmup (before measurement), send each prompt to llama-server's `/tokenize` endpoint to get exact token counts for the loaded model. Cache per model — tokenization is deterministic for a given model+prompt pair. Record exact token counts in results. Capability detection probes for `/tokenize` availability after server launch; if absent (older llama.cpp builds), falls back automatically — consistent with the capability-gating philosophy in llm-run.

**Fallback: byte-length canonical sizing.** If `/tokenize` is not available (older llama.cpp builds), prompts are sized by character/byte count only. The prompt set table below shows approximate token ranges for reference, but results record `prompt_bytes` and `prompt_chars` as the authoritative input size. Token counts in results are marked `"token_count_source": "estimated"` vs `"tokenized"`.

**Rule:** benchmark scenarios never gate on token count for job generation. Prompt sets are fixed lists of text — the "token range" in the table below is descriptive, not prescriptive.

### 10.3 Prompt Sets

| Set | Approx. Token Range | Count | Purpose |
|---|---|---|---|
| `short` | ~30–80 | 20 | Measure raw generation speed with minimal prompt processing |
| `medium` | ~400–600 | 15 | Balanced prompt processing + generation |
| `long` | ~1500–2500 | 10 | Stress-test prompt processing throughput |
| `code` | ~200–800 | 15 | Code-specific prompts (generation, explanation, debugging) |
| `reasoning` | ~300–600 | 10 | Multi-step reasoning requiring coherent long outputs |

### 10.3 Custom Prompts

Users can define custom prompt sets as text files (one prompt per `---` separator) or as YAML:

```yaml
# prompts/my-prompts.yaml
prompts:
  - text: "Explain the CAP theorem in distributed systems."
    expected_tokens: 300
  - text: "Write a Go function that implements a concurrent-safe LRU cache."
    expected_tokens: 500
  - text: |
      Review this code for bugs:
      ```go
      func merge(a, b []int) []int {
          result := make([]int, 0)
          // ... (code here)
      }
      ```
    expected_tokens: 400
```

## 11. Directory Layout

Mirrors hfetch and llm-run XDG separation:

```
LLM_BENCH_CONFIG_DIR/                  # Default: ~/.config/llm-bench
└── config.json                        # Tool preferences

LLM_BENCH_DATA_DIR/                    # Default: ~/.local/share/llm-bench
└── results/
    └── <run_id>/                      # Per-run result directories
        ├── config.yaml
        ├── results.json
        ├── summary.json
        ├── system.json
        └── jobs/

LLM_BENCH_CACHE_DIR/                   # Default: ~/.cache/llm-bench
└── tokenization/                      # Cached per-model tokenization results
```

`LLM_BENCH_HOME` is a convenience override that remaps all three (same semantics as `HFETCH_HOME` and `LLM_RUN_HOME`).

## 12. Environment Variables

| Variable | Purpose | Default |
|---|---|---|
| `LLM_BENCH_HOME` | Convenience override — remaps config/data/cache subdirs | (none, uses XDG) |
| `LLM_BENCH_CONFIG_DIR` | Config directory | `~/.config/llm-bench` |
| `LLM_BENCH_DATA_DIR` | Data directory (results) | `~/.local/share/llm-bench` |
| `LLM_BENCH_CACHE_DIR` | Cache directory (tokenization cache) | `~/.cache/llm-bench` |

Inherits hfetch and llm-run env vars for model and engine resolution.

## 13. Go Library API

```go
package llmbench

// Suite represents a loaded benchmark configuration
type Suite struct { ... }

// LoadSuite parses a benchmark config file
func LoadSuite(path string) (*Suite, error)

// Runner executes benchmark suites
type Runner struct { ... }

// NewRunner creates a runner with the given options
func NewRunner(opts ...RunnerOption) (*Runner, error)

type RunnerOption func(*runnerConfig)
func WithOutputDir(dir string) RunnerOption
func WithEngine(engine *llmrun.Engine) RunnerOption
func WithHFClient(client *hfetch.Client) RunnerOption
func WithProgressFunc(fn ProgressFunc) RunnerOption

// Run executes all jobs in the suite
func (r *Runner) Run(ctx context.Context, suite *Suite) (*RunResult, error)

// RunResult contains the complete results of a benchmark run
type RunResult struct {
    RunID       string
    Suite       *Suite
    Jobs        []JobResult
    Hardware    HardwareInfo
    StartedAt   time.Time
    CompletedAt time.Time
}

// Reporter generates output from results
type Reporter struct { ... }
func (rp *Reporter) Terminal(result *RunResult) string
func (rp *Reporter) JSON(result *RunResult) ([]byte, error)
func (rp *Reporter) CSV(result *RunResult) ([]byte, error)
func (rp *Reporter) Compare(results ...*RunResult) string

// Store persists and retrieves benchmark results
type Store struct { ... }
func NewStore(dir string) *Store
func (s *Store) Save(result *RunResult) error
func (s *Store) Load(runID string) (*RunResult, error)
func (s *Store) List(filter StoreFilter) ([]RunSummary, error)
```

## 14. Error Handling and Resilience

### 14.1 Job-Level Failures

Each job records its outcome explicitly:

```go
type JobStatus string

const (
    JobStatusOK      JobStatus = "ok"       // Completed successfully
    JobStatusFailed  JobStatus = "failed"   // Error during execution
    JobStatusSkipped JobStatus = "skipped"  // Skipped (dependency, --jobs filter, resume)
)

// JobError captures typed failure information for reporting and resume.
type JobError struct {
    Type    string  `json:"type"`    // e.g. "server_crash", "timeout", "oom", "auth.ErrAuthRequired"
    Message string  `json:"message"`
}
```

Per-job result JSON includes `"status": "ok|failed|skipped"` and `"error": {...}` (null when status is ok). This makes `--continue-from` and comparison filtering straightforward.

Failure behaviors:
- **Server fails to start:** Record `status: "failed"`, `error.type: "server_start"`, skip to next job.
- **Server crashes during benchmark:** Record partial results + `status: "failed"`, `error.type: "server_crash"`, continue.
- **Timeout:** Kill the server, record `status: "failed"`, `error.type: "timeout"`, continue.
- **OOM kill:** Detect via process exit code, record `status: "failed"`, `error.type: "oom"` with memory estimate, continue.
- **Auth errors during pull:** Record `status: "failed"`, `error.type` is the typed error name (e.g. `"auth.ErrAuthRequired"`), continue.

### 14.2 Suite-Level Resilience

- `abort_on_error: false` (default): continue past failed jobs, report failures at the end.
- `abort_on_error: true`: stop the entire suite on first failure.
- `--continue-from <job_id>`: resume an interrupted run, skipping already-completed jobs (checks `status` field).
- Results are written incrementally (per-job), so partial runs are still useful.

### 14.3 Orphan Prevention

- All child processes (llama-server instances) are tracked and guaranteed cleaned up:
  - On normal completion.
  - On SIGINT/SIGTERM to `llm-bench`.
  - On timeout.
  - Via process group management as a failsafe.

## 15. Testing Strategy

- **Unit tests:** Config parsing, metric aggregation, report formatting, job matrix expansion.
- **Integration tests:** Run a quick benchmark against a tiny model (TinyLlama 1.1B) to verify the full pipeline.
- **Mock tests:** Mock the llama-server responses to test metric collection and error handling without real inference.
- **Snapshot tests:** Verify report output format stability.

## 16. Future Considerations

- **Quality benchmarks:** Integrate with eval frameworks (lm-eval-harness) for accuracy/quality metrics alongside performance.
- **Regression tracking:** Track performance over llama.cpp versions to detect regressions.
- **Hardware comparison:** Support running the same suite on different machines and comparing results.
- **Automated scheduling:** Cron-based scheduled benchmarks with alerting on performance changes.
- **Web dashboard:** A lightweight local web UI for browsing and comparing historical results.
- **Power consumption:** Integrate power measurement (where hardware supports it) for efficiency metrics (tokens/watt).
