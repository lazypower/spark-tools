# llm-serve build — status

The llm-serve (vLLM sibling to llm-run) build and its llm-tidy eviction interlock
have **landed on `main`**. This file is no longer a cross-session pickup for
in-flight branches; it records only the remaining open items. Durable build
history and working notes live in continuity, not here.

## Shipped (on main)
- **Contract engine** — resolve a serve request → validated vLLM flags → emit
  (compose / run / quadlet), rejecting footgun combinations; warn-not-gate on
  staleness.
- **B1 lifecycle** — drive-the-driver: emit → `docker compose up` → reconcile.
  `up` / `down` / `status` / `recover`.
- **B2 liveness** — eviction protection, derived-live, fail-closed.
- **B3 interlock** — llm-tidy consults `llm-serve liveness` before pruning a
  served model.
- **vLLM backend in llm-tidy** — inventories and prunes the HF dirs llm-serve
  serves.

## Open items
1. **vLLM pull-via-sync.** `llm-tidy sync` still *skips* `vllm:` manifest entries
   (`internal/reconcile/apply.go` emits "vLLM sync not yet supported; use
   hfetch pull --dest vllm") — there is no PullVLLM. Follow-on: a vLLM syncer
   over `hfetch pull --dest vllm` so `vllm:` entries sync like the others.
2. **On-box B3 + vLLM acceptance.** GPU-only, runs on the Spark, not in CI:
   prove `llm-tidy prune` sees a vLLM model, the interlock protects a served
   one, and the unmanaged-container complaint fires.
3. **B4 probe subsystem** — deferred (v2.1, GPU-only, on-box).

## Notes
- All Go via **devbox** (`devbox run -- go build/test ./...`), zero cgo, module
  `github.com/lazypower/spark-tools`.
- The Spark is arm64: cross-compile with `env CGO_ENABLED=0 GOOS=linux
  GOARCH=arm64 go build`, and verify a deploy by sha256 match + fresh mtime on
  the box (a version-string echo can lie).
