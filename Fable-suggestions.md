# Fable-suggestions.md — Fresh-Eyes Audit of spark-tools

*Fable 5, 2026-07-07. Method: six parallel deep-read audits (one per tool + one cross-cutting),
findings independently spot-verified against the code; the flagship claims were verified
empirically against built binaries. Build, `go vet`, and the full test suite pass clean on main
(5764119). Line numbers reference that commit.*

---

## The cold read

This is a codebase whose engineering culture outruns its product truth.

The internals are genuinely excellent — better than most professional repos I see. The hub
client's hash discipline (LFS SHA256 vs git-blob SHA1 as a named, tested distinction), the vLLM
completeness gate, llm-serve's fail-closed lifecycle design, the tidy↔serve eviction interlock,
the `pkg/seam` contract-test tripwire idea, a 7-dependency go.mod, and a disciplined
mid-refactor state with deprecation shims that carry delegation tests — all of this shows a
maintainer who thinks in invariants. The newer the code, the truer this is: llm-serve reads
like it was built under adversarial review, because it was.

But the user-facing shell has decayed away from what the product *says* it is, and that gap is
now the dominant risk. Concretely:

- **The advertised front door doesn't work.** `hfetch org/model` — the README's third example,
  spec §8.1's flagship interaction — errors with `unknown command "foo/bar"`. Verified against
  a fresh build. The shorthand branch in `cmd/hfetch/main.go:30` is dead code (no `Args:`
  policy on a root command with subcommands), and it would fail on an empty `--profile` even
  if it were reachable.
- **The trust-building command doesn't build trust.** `hfetch verify` — the "cron-able bitrot
  sweep" — hard-fails every GGUF model (it unconditionally runs the safetensors gate) and never
  re-hashes a GGUF file at all. The tool's primary artifact type is unverifiable.
- **The benchmark's numbers include fabrications.** `mean_cpu_pct` is always 0 (no CPU sampling
  code exists in `metrics/`), `peak_memory_mb` reports **total system RAM** (parses `MemTotal:`),
  and both are presented as `available: true`.
- **The chat TUI corrupts its own conversation.** `/stats` output — a lipgloss box, ANSI escapes
  included — is appended as a `system` message and sent to the model on every subsequent turn.
  `/temp` validates, confirms, and does nothing.
- **The README describes a product two generations old.** Six binaries ship; the README says
  "four tools" and omits `llm-chat` and `llm-serve` entirely. CLAUDE.md names a *different*
  four. The primary install command (`devbox run build`) compiles everything and produces zero
  binaries. `llm-tidy` is marketed in the README and absent from every release artifact.

None of these are hard fixes. They're all the same failure mode: features shipped, then the
surface that promises them was never re-checked. The repo has strong machinery for catching
*internal* contract drift (seam tests) and nothing that catches *product* drift — the drift
between what `--help`, the README, and the specs promise and what the binaries do.

The second structural observation: the suite has quietly become **six tools with three
unexplained overlaps** — `llm-run serve` vs `llm-serve`, `llm-run chat` vs `llm-chat`,
`hfetch gc`/`rm` vs `llm-tidy prune`. Each split is individually defensible (I'd keep all
three), but the discriminators live only in your head. The GGUF→llm-run / safetensors→llm-serve
routing rule is written down nowhere user-facing, and `llm-serve --help` still claims the tool
is "emit-only" while registering `up`/`down`/`recover`. A newcomer's first five minutes are:
install command produces nothing → two tools aren't in the README → "which serve do I use?"

Verdict: **the foundation deserves more shell than it has.** Prioritize truth-telling over new
capability: make every command either do what it says or say what it does. The highest-leverage
week of work on this repo is not a feature — it's closing the ~15 places where output and
behavior diverge, and writing the one README paragraph that routes users between the six tools.

---

## Cross-tool defect patterns

Fresh-eyes value is mostly here: five recurring classes, each appearing in 3+ tools.

### 1. Display/execute divergence (worst class — the tools lie)
| Where | What |
|---|---|
| `cmd/llm-tidy/sync.go:55-72` | `sync --backend ollama` prints a filtered plan, then `tidy.Sync()` executes the **unfiltered** diff — will pull tens of GB of GGUF you excluded. Verified. |
| `cmd/llm-bench/run.go:121,137` | On Ctrl-C prints "Results saved to: …" but `SaveRun` never runs (`suite/runner.go:153-158`); the run is invisible to `results list`/`compare` forever. |
| `cmd/llm-tidy/init.go:29,49-58` | `init --backend ollama` reports "0 GGUF models" while writing all GGUF specs into the manifest anyway. |
| `internal/modelstore/manifest.go:166-210` | `hfetch rm org/typo` prints "Removed" on a complete no-op. |
| `cmd/llm-run/explain.go:182-242` | `explain effective` — whose entire purpose is truth — prints a command containing `--reasoning-budget 0` that `chat`/`serve` would never run (they fix it up to `-1`). |
| `internal/tui/chat.go:350-364` | `/temp 0.7` → "Temperature set to 0.70" → value stored nowhere, request never carries it. |
| `internal/reconcile/apply.go:101-106` | `sync` dry-run lists vLLM specs as "will be pulled"; real sync silently skips them (the skip event is filtered out at `pkg/llmtidy/llmtidy.go:204-212`). |

### 2. Dead config — knobs wired to nothing
- **hfetch:** `pull --verify` and `files --min-size/--max-size` defined, never read
  (`cmd/hfetch/pull.go:61`, `files.go:101-102`). `hfetch config set` writes prefs no code path
  ever loads.
- **llm-bench:** YAML `timeout` (parsed at `suite/scheduler.go:48`, enforced nowhere — see
  P0-5), `system_check`, `output_formats`, `cooldown_between` (used in the spec's own example
  *and* in `llm-bench init` output!) all dead; `--dirty-mode` CLI flag always loses to the
  defaulted config value (`suite/runner.go:111-115` — precedence inverted).
- **llm-run:** `--system` becomes a llama-server CLI flag that never reaches the OpenAI
  conversation (`pkg/llmrun/engine/config.go:123-125`); profile save/edit covers ~5 of the
  RunConfig fields, so most settings can't be persisted (`cmd/llm-run/profile.go:92-191`).

### 3. Zero-value sentinel traps — "0 means unset" makes legitimate zeros unrepresentable
- `gpuLayers: 0` (CPU-only — the exact remedy `engine/config.go:28`'s error message suggests)
  is flipped to all-layers by `applyDefaults` (`cmd/llm-run/chat.go:325-353`).
- Temperature 0 (greedy) silently becomes 0.7; booleans only ratchet on (a profile can't turn
  flash-attn off); `reasoningBudget: 0` (`--no-think`) can't be saved in a profile.
- llm-bench: `warmup_prompts: 0` / `cooldown_seconds: 0` are coerced back to defaults
  (`suite/config.go:92-106`) — warmup can't be disabled.
- `defaults_test.go:12` asserts "user values not overwritten" but only tests non-zero values —
  exactly missing the hole.

### 4. Non-atomic writes to load-bearing state
All plain `os.WriteFile` truncate-then-write, no temp+rename, no locking:
- llm-tidy manifest (`internal/tidymanifest/manifest.go:102`) — the sole guard against deletion.
- hfetch registry manifest (`internal/modelstore/manifest.go:88`) — corruption bricks *every*
  hfetch command until hand-deleted (`Load` hard-errors), and concurrent processes silently
  drop each other's registrations.
- llm-bench results (`store/store.go:63-144`) — and every `SaveJob`/`SaveRun` error is
  discarded at the call sites; a full disk mid-run loses 6 hours silently.
- GGUF shard merge writes directly to the final path (`internal/gguf/merge.go:65`); an
  interrupted multi-GB merge leaves a truncated file that the next `ollama-import` **reuses via
  a size>0 check** (`cmd/hfetch/ollama.go:223-226`).

### 5. Non-strict YAML at trust boundaries
Zero uses of `KnownFields(true)` in the repo. Consequences ranked:
- llm-tidy manifest: a misspelled section (`guff:`) parses clean, unblesses every model in it,
  and a cron'd `prune --yes --older-than 30d` (the spec's own suggestion) **deletes previously
  blessed models**. The manifest is a deletion-safety boundary; it must reject unknown keys.
- llm-bench suite: `warmup_promts: 10` silently benchmarks with defaults.

---

## Priority list

### P0 — trust and data safety
1. **llm-tidy manifest boundary** — strict decoding + atomic write + reject `--older-than <= 0`
   (`cmd/llm-tidy/util.go:24-31` accepts `-7d`, and `plan.go:23` treats non-positive as *no
   cutoff* — a typo prunes everything untracked). Also: zero `Modified` timestamps are treated
   as infinitely old (`plan.go:31`) — "age unknown" should mean protected.
2. **llm-tidy `sync --backend`** — thread the filter through `Tidy.Sync` (pattern #1 above).
3. **hfetch `verify`** — verify what the manifest says was downloaded: re-hash GGUF files at
   their recorded `LocalPath` (it currently ignores `LocalPath` entirely, so even
   `pull --dest vllm && verify` fails with "not downloaded" — `verify.go:57-62`), and apply the
   safetensors gate only where it applies.
4. **hfetch bare-arg shorthand** — `Args: cobra.ArbitraryArgs` on root + default the profile in
   `runPull`. It's the README's advertised interaction.
5. **llm-bench: enforce the job timeout.** A llama-server that accepts the connection and stops
   streaming hangs an unattended overnight run forever (`job/probe.go:58` uses
   `http.DefaultClient`, no timeout; `ExecParams` has no timeout field). This defeats the
   tool's stated purpose. Same pass: fix the sampler (CPU never sampled, `MemTotal` parsed as
   peak usage — `metrics/system.go:124-164`; the correct parse already exists in
   `syscheck/resources.go:139`) or report `available: false` honestly.
6. **llm-bench: finalize interrupted runs** and fix `--continue-from` (a non-matching job ID
   silently skips the *entire suite* — `suite/runner.go:161-177`).
7. **TUI transcript pollution** — give `/stats`, `/context`, `/temp`, `/save` a display-only
   channel instead of `Role: "system"` messages that get sent to the model with ANSI codes
   (`internal/tui/chat.go:316-332`, request at `:452`). Then make `/temp` real or delete it.
8. **llm-run PID file** — key by port (or replace with a port probe). One global `server.pid`
   (`pkg/llmrun/engine/launch.go:39`) makes chat/serve/run/bench mutually exclusive — llm-bench
   is a victim of its own sibling — and the "already running" error reports the *new* request's
   port, not the running server's (`launch.go:158`).
9. **llm-tidy: tolerate a missing backend.** Spec §5.3 promises skip-with-warning; today an
   unreachable Ollama daemon makes `prune` and `sync` entirely unusable on a GGUF-only box
   (`pkg/llmtidy/llmtidy.go:151-162` treats partial inventory as fatal; only `status` tolerates
   it).
10. **hfetch merge safety** — temp+rename, validate `split.count` == shards provided (currently
    never read — `merge.go:37-41` — so a missing shard merges "successfully"), and stop
    trusting any size>0 pre-existing merged file.

### P1 — product coherence
11. **README/CLAUDE.md tell the truth**: six tools, a "which tool when" router paragraph
    (GGUF→llm-run, safetensors→llm-serve; endpoint-only→llm-chat), fix `devbox run build`
    (emits no binaries), document llm-tidy's vLLM backend (`internal/tidymanifest/manifest.go:24`
    — undocumented in README, unvalidated by `Validate`, mislabeled in every `--backend` help
    string as `(ollama|gguf)`).
12. **Ship what you market**: add llm-serve + llm-tidy to justfile, CI build, release.yml,
    Homebrew formula, and `.gitignore` (llm-serve is in none of them; llm-tidy is in the README
    but has never been released).
13. **llm-serve help text**: root Long still says "Emit-only" above the registration of
    `up`/`down`/`status` (`cmd/llm-serve/main.go:23-27`); same stale story in
    `pkg/llmserve/llmserve.go:7-9` and `servespec/emit.go:230-233`.
14. **Delete or rewrite `specs/SESSION-HANDOFF.md`** — "all OPEN, none merged to main" is false;
    the designated fresh-session entry point now actively misleads. Its real open items (vLLM
    sync skip, on-box B3 acceptance) deserve a live home.
15. **Finish the extraction** — migrate `cmd/*` off the deprecated `pkg/*` compat shims and
    delete them. Until then every authority has two import paths, and the newest code
    (llm-chat) imports the deprecated one. Also collapse the two known competing paths the
    extraction map itself flags: CLI `runPrune` bypasses `Tidy.Prune`'s interlock gate
    (`cmd/llm-tidy/prune.go:86,120` — duplicate gate, hardcoded checker, `WithChecker` ignored),
    and CLI `runPull` vs `Client.Pull` (the vLLM profile/gate exists only in the CLI —
    violates your own library-first rule; `PullOptions` has no `Profile`).
16. **Wire the model into "smart defaults."** Every `RecommendConfig` call passes `nil`
    metadata (`cmd/llm-run/serve.go:82`, `chat.go:159`, `hw.go:93`) even though the resolver
    already parsed the GGUF header. Result: every model on a 128 GB Spark gets `--ctx-size
    32768`, including 4K-trained models — silent quality degradation past the trained window,
    and the KV-cache math in `recommend.go` is dead code. (When wiring it: the formula at
    `recommend.go:103` ignores GQA — overestimates KV by 4-8× on modern models.)
17. **llm-chat viability pass** (details below): `bubbles/textarea` + `bubbles/viewport`,
    startup health probe, stop sending the endpoint URL as the model name, fix the submit key.

---

## Per-tool findings

Curated to what's actionable; severity/confidence from verified code reads.

### hfetch

Strong core (hub client, downloader finalization, completeness gate — the best-tested code in
the repo), weak shell. Beyond P0 items 3, 4, 10:

- **`gc` deletes the user's `--keep` merged file** — the merged GGUF is never registered in the
  manifest (spec 04 §6.4 says it should be), so `gc` sees it as orphaned
  (`internal/modelstore/gc.go:61-67`). `gc` also unconditionally deletes `.partial`/`.state`
  with no age check or locking — running it during another hfetch's download destroys the
  resume state mid-flight (`gc.go:51-57`).
- **Resume refused when disk is "too small"**: the space pre-check demands the *full* file size
  before reading resume state (`internal/download/manager.go:107-114`) — a 99%-complete 100 GB
  resume needing ~1 GB is permanently refused.
- **Completeness gate blind spot**: `resolveDest` flattens by basename (`pull.go:79`), so
  `nvidia/X` and `meta-llama/X` collide into one directory, and the gate never scans for
  *extra* `*.safetensors` the repo doesn't ship — vLLM globs them at serve time. This is the
  exact mixed-weights failure the gate exists to prevent. Add a stale-weights sweep (the code
  already does this for tokenizers, `completeness.go:286-289`) and include the org in the dir
  name.
- **Pre-existing final file trusted blindly** (`manager.go:102` — any size>0 file short-circuits
  the download) and then registered `Complete: true` with the *upstream* hash as provenance
  (`pull.go:379-387`) — recording a hash the local bytes may not match.
- **401 without a token says "token is invalid"** (`internal/hub/client.go:388-390`) — should be
  `ErrAuthRequired` when no token was sent; only `WhoAmI` gets this right. Also `whoami` exits
  0 on an invalid token (`auth.go:115-121`), so scripts can't gate on it.
- **`rm` has no confirmation** and an unused `Confirm` helper sits right there in
  `internal/ui/picker.go:62`.
- **`files` on a safetensors repo** collapses everything into one `—` quant row with a summed
  size and no filenames (`files.go:61-93`). `GroupByQuant` also conflates two different GGUFs
  sharing a quant string into one "(2 files)" pick (`internal/gguf/filter.go:86`).
- **`path` returns the first complete file** (`manifest.go:117-135`) — for a vLLM model that
  can be `config.json`; spec says largest GGUF.
- **Fit estimation never runs**: `EstimateFit` is only called with `meta=nil, availableGB=0`;
  unless `HFETCH_VRAM` is set, every fit column is blank — on hardware you *know* has 128 GB.
- Dead/broken library API: `FetchGGUFMetadata` fetches 8 KB then tries to parse *all* KVs —
  fails on any model with embedded vocab; zero callers (`pkg/hfetch/hfetch.go:145-152`).
- Test gaps where it hurts: no end-to-end resume test; `cmd/hfetch/ollama.go` (622 lines of
  heuristics) has zero tests; no gate-against-GGUF-repo test (would have caught the verify bug).

### llm-run

The weaker half of the serving story. Beyond P0 items 7, 8 and P1 item 16:

- **Alias cycles crash**: `alias set a a` then `chat a` → unbounded recursion → stack overflow
  (`internal/modelref/resolve.go:183-201`; `SetAlias` validates nothing).
- **BuildCommand warnings are discarded** (`engine/launch.go:26`: `args, _, err :=`). Because
  the capability probe can't see a positive `--mmap` flag, a warning is generated and dropped
  on *every launch*. Users are never told requested features were disabled. The launch →
  watcher → WaitForReady → error dance is also copy-pasted three times
  (`chat.go:218-242`, `chat.go:264-288`, `serve.go:138-167`) — extract `LaunchAndWait` and
  surface warnings in one place.
- **Cold-load timeout UX**: chat waits silently with a 60s default (`chat.go:203`); a healthy
  70B cold load dies with "timeout after 1m0s" and no `--timeout` hint. llm-serve's warning
  text ("large models cold-start in minutes…") is the model to copy.
- **`llm-run raw` SIGKILLs llama-cli on first Ctrl-C** (`raw.go:83-91`,
  CommandContext default) — destroying llama-cli's own interrupt-generation semantics in the
  "escape hatch" command.
- **NUMA detection primary path is dead**: `exec.Command("ls", "-d", ".../node*")` — no shell,
  no glob expansion; always falls through to lscpu (`hardware/detect.go:316`).
- **`--api-key` from a profile 401s chat**: server launches with `--api-key`, but the TUI
  client is built without it (`cmd/llm-run/chat.go:244,303`), and `chat` has no `--api-key`
  flag. Health polling passes (exempt from auth), then every completion fails.
- Spec 02 promises ~a third more product than exists: auto-pull (`hf://` parses, never pulls),
  port auto-select, capability cache (`DetectBinaries` re-probes every invocation), restart
  policy, model-aware recommend. Either implement or trim the spec — it's the source of truth
  by your own convention.
- Two sources of truth for "what models do I have": `models` walks the filesystem re-deriving
  names from `--` encoding (`models.go:95-137`); the resolver uses the registry manifest. They
  disagree on incomplete downloads and nonstandard paths.
- Riskiest code, least tested: zero tests for `checkPIDFile`, `Stop` escalation,
  `WaitForReady`, warning propagation.

### llm-serve

The strongest tool in the repo — the B1/B2 discipline shows and the tests attack real failure
modes. Three edge-of-model gaps:

- **Conflict detection can't see hand-launched stacks**: `Inspect` filters on
  `managed-by=llm-serve` (`pkg/llmserve/runtime/compose.go:103-112`), so the B1-promised
  "hand-launched ⇒ conflict ⇒ refuse adoption" can never fire; you get a port-bind fight inside
  compose instead. (B2 liveness correctly uses unfiltered `docker ps` — the asymmetry is the
  tell.)
- **Warmup predicate vs thinking models**: the prober requires non-empty `message.content` with
  `max_tokens: 16` (`runtime/prober.go:49-92`); with `--cap thinking`, vLLM routes those tokens
  to `reasoning_content`, the predicate never holds, and after 20 minutes `up` **tears down a
  healthy instance**. Accept `reasoning_content` as evidence or disable thinking for the probe.
- **Recovery is impatient with interrupted loads**: Ctrl-C an `up` 7 minutes into an 8-minute
  cold load, run any mutating command — `recoverLocked`'s one-shot reconcile sees "not serving"
  and destroys the nearly-ready stack (`lifecycle/lifecycle.go:377-383`), forfeiting exactly
  the patient-load property `waitServing` was built for. Also: one corrupt manifest makes
  `Store.List` fail and recovery **silently no-op** for all instances
  (`lifecycle.go:367-370`).
- Smaller: `up` can't express `--dtype` but `emit` can (`plan.go:74-79` never sets it);
  `status` sends a real 16-token completion to every instance — a monitoring loop becomes
  synthetic GPU load; unquoted compose volume lines break on paths with spaces
  (`servespec/emit.go:250`).

### llm-bench

Right architecture, correct math (aggregator percentiles/stddev verified — textbook), but the
instrument's numbers aren't currently publishable. Beyond P0 items 5, 6:

- **Methodology: "context-scaling" doesn't measure long context.** `context_sizes:
  [4096..32768]` only changes the allocated KV cache; the built-in prompts stay ~60-300 tokens
  (measured: `medium` ≈ 60 tokens vs documented "~400-600"; `long` ≈ 315 vs "~1500-2500" —
  `prompts list` repeats the wrong numbers). You're benchmarking near-empty-context inference
  at different allocation sizes. Either generate prompts that fill the context or rename the
  scenario.
- **`compare` collapses scenarios, context sizes, and repeats into one cell** keyed only by
  (model, quant) (`report/compare.go:49-60`) — repeats aren't pooled (runs 2-3 discarded), and
  a ctx=4096 job silently competes with a ctx=32768 job for the same cell. Comparison output
  can be actively misleading. Key by scenario_id and pool repeats.
- **Parallel mode drops failures silently**: failed probes vanish from the collector
  (`job/job.go:256-260`) — 7-of-8 failed requests reports "ok" with n=1; and
  `promptsPerSlot := total/slots` floors (8 prompts when you asked for 10). No aggregate
  throughput or queue metrics, so concurrency scenarios can't answer their headline question.
- **`quick --ctx abc` panics** (index-out-of-range, `cmd/llm-bench/quick.go:40-66`); multiple
  `--ctx` values parse and all but the first are silently discarded.
- **`scenario_id` hashes the prompt-set *name/path*, not content** (`suite/scheduler.go:64-85`)
  — editing prompts in place keeps runs "comparable". The doc comment claims otherwise.
- **Preflight failure prints the wrong check's message** — always `Results[0]` (idle), so a
  thermal failure reports "CPU idle (3.2% utilization)" (`suite/runner.go:116-119`).
- Missing models: no auto-pull and no preflight existence check (spec promises both) — a
  typo'd quant burns cooldown after each of 40 identical failures (~7 min to report one typo).
- `--output-dir` results are orphaned: `results/compare/report` always read the default XDG dir.
  `quick` results are never persisted at all.
- Verified-correct and worth keeping as-is: per-job server teardown is fully synchronous with
  SIGTERM→SIGKILL escalation and PID hygiene; sampling window (post-warmup, pre-teardown) is
  right; timings come from llama-server's own counters.
- Zero tests for `Executor.Execute` — the entire launch/warmup/measure/teardown pipeline (spec
  §15 explicitly calls for mock-server tests).

### llm-chat + shared TUI

Great concept, currently a demo. Three walls in the first five minutes: possibly can't send a
message at all, loses scrollback after one screen, and silently breaks against the endpoints it
advertises. Beyond P0 item 7 (transcript pollution — shared with llm-run chat):

- **Submit key**: multiline mode's only submit is Alt+Enter (`internal/tui/chat.go:112-117`;
  the doc comment says Ctrl+Enter, `chat.go:27`). On default macOS Terminal/iTerm2 (Option not
  sending Meta) there is **no way to send a message**. llm-chat hardcodes `MultiLine: true`
  with no flag (`cmd/llm-chat/main.go:47`).
- **Endpoint URL sent as model name**: `if model == "" { cfg.ModelName = endpoint }`
  (`main.go:50-52`) → `"model": "http://host:8080"` in every request. llama-server ignores it;
  vLLM and gateways 404. Query `/v1/models` at startup (the client already has `ListModels`)
  or omit the field. Display name and wire field are two questions in one variable.
- **No scrollback**: full-transcript render in alt-screen — past one screenful, older messages
  are unreachable forever. No viewport, no PgUp, no mouse.
- **Hand-rolled input is the root cause cluster**: backspace deletes *bytes* (corrupts é/CJK/
  emoji — `chat.go:118-121`), pasted multiline text injects raw `\r` (paste runes unfiltered —
  `chat.go:122-123`), no cursor movement/history (a `history` field exists, never used). One
  refactor to `bubbles/textarea` + `bubbles/viewport` (bubbles is already in the module graph
  via huh) erases this whole cluster.
- **Streaming robustness**: mid-stream server error payloads unmarshal into an empty delta and
  vanish (`internal/openaiapi/client.go:209-211` — `StreamDelta` has no `Error` field; user
  sees an empty reply, no error); `data:` without a space (SSE-legal) skips every event;
  5-minute whole-request `http.Client.Timeout` kills long generations (`client.go:38`) and the
  error path discards all partial output (`chat.go:161-165` resets the buffer uncommitted);
  no `stream_options.include_usage`, so against spec-compliant servers `/stats` is permanently
  zero (works today only because llama-server volunteers usage).
- **Boundary truncation never truncates**: the computed `clean` prefix is discarded into `_`
  (`chat.go:475-480`), so the leaked role-marker fragment the comment promises to discard is
  committed to the transcript. The boundary tests cover the pure function, not the streaming
  path where the bug lives.
- Smaller: `/System <prompt>` sets the system prompt to the literal string including the
  command (case-sensitive TrimPrefix after case-insensitive match, `chat.go:291,335`);
  `View()` clears errors on any render, so a resize eats an unseen error (`chat.go:240-245`);
  `/context` shows `NaN%` in llm-chat (ContextSize never set, no zero guard); Ctrl-C
  mid-stream kills the whole session instead of cancelling the generation; no `/help`; the
  five stop-sequence tokens are maintained in three places (`tui/chat.go:383-391`,
  `cmd/llm-run/chat.go:307-310`, `cosmeticTokens`) — one authority, please.
- No spec exists for llm-chat, so none of these gaps are even classifiable as deliberate.

### llm-tidy

Best safety engineering in the repo where it counts (plan-shown-before-delete, faithful prune
dry-run, per-model failure isolation, the interlock's fail-closed discipline, `VLLMDelete`'s
careful mixed-repo handling). The exposure is at the manifest trust boundary — P0 items 1, 2,
9 — plus:

- **Registry-port names break `:latest` normalization**: `myreg.example.com:5000/team/model`
  has a colon, so normalization is skipped, Ollama reports `...model:latest`, exact-match diff
  unblesses it → prune deletes a blessed model (`internal/tidymanifest/manifest.go:56`). Check
  for a colon *after the last slash*.
- **Ollama matching is case-sensitive while GGUF is case-insensitive** (`diff.go:136` vs
  `:144`) — spec-compliant but an unexplained asymmetry, and hand-edited manifests (the
  workflow `init` explicitly tells users to follow) silently unbless on case drift.
- **`init` silently overwrites a curated manifest** (`pkg/llmtidy/llmtidy.go:337`) — weeks of
  curation gone, no prompt, no backup.
- **`Registry.Remove` ignores file-deletion errors** (`internal/modelstore/manifest.go:194`) —
  on EACCES the file stays, the registry forgets it, prune counts its bytes "reclaimed".
- Mixed-repo sizes double-count (`VLLMList` sums `.gguf` files that `GGUFList` also lists),
  and prune's "reclaimed" overstates for vLLM rows since the `.gguf` is preserved.
- TOCTOU between plan display and apply: a `promote` in another terminal during the confirm
  prompt is not re-checked; cheap fix is a re-diff after confirmation dropping newly-blessed
  candidates.
- Test gaps mirror the bugs exactly: no end-to-end `sync --backend` test, no
  unreachable-backend prune test, no negative-duration test, no unknown-key manifest test.

---

## What's genuinely strong (keep doing this)

- **`pkg/seam`** — cross-boundary contract tests targeting the repo's actual historical bug
  classes. Rare and valuable. One fix: all 11 tests pass while `doc.go` and two test comments
  still say "intentionally RED" (`library_pull_test.go:25`, `registry_inventory_test.go:17`) —
  stale STATUS markers invert the tripwire's value.
- **llm-serve's lifecycle design** — atomic manifests, derived-never-stored readiness,
  crash-loop-aware waiting, fail-closed liveness. The interlock echo-match contract
  (verbatim path echo, canonicalize-only-for-comparison) is exactly right.
- **The extraction refactor's discipline** — alias shims with delegation tests, honest live
  status docs. Just finish it.
- **Dependency hygiene** — 7 direct deps, one YAML lib, stdlib HTTP. Guard it.
- **Test culture** — only 6 of ~60 packages lack tests entirely; the suite is green. The gap
  is *placement*: coverage is thickest in pure functions and thinnest in the cmd-layer glue
  and process-lifecycle paths, which is where nearly every defect in this document lives.

---

## Suggested sequencing

1. **Week of truth-telling** (P0 1-10): every item is small, none conflicts, and together they
   convert the suite from "prints success" to "is trustworthy". The llm-tidy trio (strict
   YAML, atomic writes, duration validation) and the hfetch verify fix are the ones I'd do
   first — they guard irreplaceable data.
2. **Docs/release pass** (P1 11-14): one sitting. README router paragraph, six-tool table,
   justfile/CI/release/gitignore additions, delete SESSION-HANDOFF, fix llm-serve help.
3. **Finish the extraction** (P1 15): delete the compat shims, collapse the two competing-path
   pairs (tidy prune gate, hfetch pull), migrate llm-chat to `internal/openaiapi`.
4. **The two product bets** (P1 16-17): model-aware defaults (the pillar llm-run is named for)
   and the llm-chat bubbles refactor (one change erases five defects and three UX walls).
5. Then llm-bench's measurement validity (timeout, sampler, compare keying, honest prompts) —
   after which its numbers are worth publishing, which is presumably the point of the tool.
