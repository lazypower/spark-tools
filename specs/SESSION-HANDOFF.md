# llm-serve build — session handoff (fresh-session pickup)

Truth lives at runtime. This is the durable state of the llm-serve (vLLM sibling
to llm-run) build in the spark-tools monorepo.

## Done — all codex-gated (Mode-A design + Mode-B code, codex = cross-model adversary)
| Slice | What | PR |
|---|---|---|
| v1 (A) | emit-only contract engine (resolve → validated vLLM flags → emit compose/run/quadlet; warn-not-gate staleness) | #2 |
| B1 | runtime lifecycle, drive-the-driver (emit → `docker compose up` → reconcile; no native runtime/watchdog — reuse the watchdog sidecar) | #3 |
| B2 | liveness authority (eviction protection, derived LIVE, fail-closed) | #4 |
| B3 | llm-tidy eviction interlock (CLI shell-out to `llm-serve liveness --check`) | #5 |
| vLLM backend | llm-tidy inventories+prunes the HF dirs llm-serve serves — gives B3 its teeth | #5 |

B4 probe subsystem = **v2.1, deferred** (GPU-only, runs on the Spark not CI).

## Branches / PRs (stacked, all OPEN, none merged to main)
`#2 feat/llm-serve-contract-engine → #3 feat/llm-serve-B1 → #4 feat/llm-serve-B2 → #5 feat/llm-serve-B3` (HEAD `7df3148`). Push to BOTH remotes: **github** (github.com/lazypower) + **origin** (gitea.wabash.place). PRs via `gh` on github.

## Deployed (sha-verified Jun 28) — tensor-warden:~/.local/bin/
- `llm-serve` **v0.3.0-47-g8c6909a** (has `--check`; unchanged since — later commits were llm-tidy-only).
- `llm-tidy` **v0.3.0-50-g7df3148** (vLLM backend + interlock + LLM_SERVE_BIN). sha256 `4236224e62c605b1…d616ea`.
- `~/llm-serve` and `~/llm-tidy` are ABSENT (operator moved both to `.local/bin`).
- Interlock needs `LLM_SERVE_BIN=$HOME/.local/bin/llm-serve` (or `.local/bin` on PATH) in cron/non-interactive contexts.
- `llm-serve-instructions.md` is on the box (operator doc for infra-claude).

## Verified vs NOT-yet-verified on-box
- **Verified:** B1 (up→confirmed-serving ~479s cold start, clean down, fail-closed cleanup, staleness warn). B2 positive path + the run.sh coexistence fail-closed (unmanaged container ⇒ AllProtected).
- **NOT yet:** the vLLM-backend B3 acceptance — llm-tidy prune sees a vLLM model, the interlock protects a served one, the unmanaged complaint fires. Slice is deployed (v0.3.0-50), ready for the pass.

## Open items / next moves
1. **On-box B3+vLLM acceptance** (infra-claude runs it on the Spark; relay → fix → reship).
2. **vLLM pull-via-sync**: `llm-tidy sync` currently SKIPS `vllm:` entries (no PullVLLM). Follow-on: a vLLM syncer over `hfetch pull --dest vllm`.
3. **Merge the 4-PR stack** once v1 lands on main.
4. **Shared-contract-surface refactor** — PARKED (continuity: spark-tools-shared-contract-surface-refactor). The cross-tool seams (llm-bench→llmrun/api, llm-tidy→llm-serve) want a contracts/api layer. Not now.
5. **claude gitea identity** — parked; whether to author commits as claude@wabash.place, a dedicated session (continuity: claude-gitea-identity).

## How to work (carry forward)
- All Go via **devbox**: `devbox run -- go build/test ./...`. Zero cgo. Module `github.com/lazypower/spark-tools`.
- **codex-loop**: codex is the BREAK seat, CROSS-MODEL only (never Claude-reviews-Claude as the gate). Driver: `python3 ~/.claude/skills/codex-loop/codex_break.py --prompt-file X --effort medium --silence 150 --wall 480`.
- **Cross-compile + ship**: `env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` (Spark is arm64). **VERIFY a deploy by sha256 match + fresh mtime on the box, NOT a version-string echo** — earlier scps silently no-op'd while returning phantom "ok" (SSH-agent flakiness). Use `scp -v`, read back the box sha256, assert it equals local. ssh/scp need `dangerouslyDisableSandbox: true`; retry on agent-comm failures.
- **Key settled decisions:** drive-the-driver (no native runtime ownership); liveness derived live (no ledger/daemon); coexistence policy = trust a label / lock-and-complain on an unlabeled container mounting the models path (no container introspection); B3 seam = CLI shell-out.
- **Calibration (Chuck):** terse, verdict-first, full autonomy on design+implementation, validates via the adversarial gate, parks when not clean. Don't escalate trivial-default decisions. Prefer the simplest safe primitive over importing a spec's machinery.

## Continuity memories to recall
llm-serve-v1-built · llm-serve-b1-built · llm-serve-b2-built · llm-serve-b3-built · llm-tidy-vllm-backend-built · llm-serve-coexistence-liveness-policy · verify-deploys-by-bytes-not-command-echo · simplest-safe-primitive-not-spec-language · dont-escalate-trivial-defaults · spark-tools-shared-contract-surface-refactor
