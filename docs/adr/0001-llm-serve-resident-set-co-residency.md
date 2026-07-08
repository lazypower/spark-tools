# ADR 0001 — llm-serve resident-set co-residency

**Status:** Proposed
**Created:** 2026-07-07
**Component:** `llm-serve` (with an input dependency on `llm-run/hardware`)
**Supersedes / relates:** the `docs/internal-extraction-map.md` "engine dimension"
direction (orthogonal — see Consequences)

---

## Context

On a 128 GB DGX Spark, only one model is servable at a time today. The instinct
is to blame vLLM ("an instance commits to one model"), but that is not the
binding constraint. The binding constraint is **llm-serve's single-slot
lifecycle**:

- `up` on a second model does not add an instance — it *replaces* the first.
  `lifecycle.replace` is *"the destructive current+candidate transaction (single
  port ⇒ no overlap)... current is torn down, candidate brought up"*
  (`pkg/llmserve/lifecycle/lifecycle.go:145`). `Runtime.Down(current)` frees the
  port before `Runtime.Up(candidate)`.
- The host port is already per-instance (`--port`, default 8000, threaded through
  `emit`/`up` → `-p <port>:8000`, `pkg/llmserve/plan.go:45`,
  `internal/servespec/emit.go:211`).
- The instance store is already a *list* of named manifests
  (`internal/serveinstance`), not a single slot. It was never the store that was
  single-model.
- `gpu-memory-utilization` is **not exposed** anywhere — not a capability, not an
  emit flag. (Same shape as the `--dtype` gap: settable via `emit`, not via `up`.)

The opportunity: several vLLM instances *can* run concurrently on one box at
different ports if each caps its memory fraction so they don't each grab ~90% of
the pool. On 128 GB unified memory, e.g. a ~45 GB coder model + a ~16 GB VL model
+ a small model co-reside with headroom.

The cost that shapes the design: a vLLM cold start is ~8 minutes here. That makes
Ollama-style **swap-on-demand** painful — you pay the cold start on every switch.
The valuable behavior is therefore **co-residency** (keep the models that fit
resident, serve them all at once) and **swap only the big ones** that can't fit
alongside.

The load-bearing safety primitives already exist and are the strongest code in
the repo: confirmed-serving (`waitServing`), derived-live fail-closed liveness
(B2), atomic instance manifests, and confirmed-only teardown (B3 interlock).

## Decision

Introduce a **resident-set** orchestration layer over the existing lifecycle. It
owns exactly two things: **the memory budget** and **the `:800x` port map** for a
set of co-resident instances. Concretely:

1. **Promote the lifecycle from single-slot to a set.** `recover`/`status`/
   reconcile move from "*the* instance vs the runtime" to "the *desired set* vs
   the runtime." Bringing up `B` must not tear down a healthy `A`; it adds a
   member on its own port. This is the real design change — not the budgeter.
2. **Add a memory budgeter.** Given a desired set and the pool, assign each
   member a `gpu-memory-utilization` fraction covering its weights + KV headroom,
   such that `Σ util_i + reserve ≤ 1`. **Fail closed** if the set doesn't fit —
   refuse the whole `up`; never silently overcommit into an OOM.
3. **Expose `gpu-memory-utilization`** in the contract/emit vocabulary so the
   budgeter can set it per member.
4. **MVP is static.** `llm-serve fleet up <manifest>`: budget the fractions,
   allocate ports, drive N× the existing `up`, atomically (all serving or rolled
   back). Defer dynamic evict-to-fit to phase 2.

### The budget model

```
pool_reserve  = fixed system headroom (container + OS + non-vLLM)
util_i        = (weights_i + kv_headroom_i) / pool
admit set S   iff  Σ_{i∈S} util_i  ≤  1 − pool_reserve
```

`kv_headroom_i` is not a guess: the GGUF-parsed, GQA-aware KV-cache estimator
added in P1-16 (`pkg/llmrun/hardware/recommend.go` — `layers × kv_heads ×
(key_dim+value_dim) × 2 × ctx`) is exactly the "how much KV must this member
reserve" function. The budgeter reuses it. This is a genuine synergy, not a
coincidence — the same estimator that sizes a single model's context sizes a
member's memory share.

## Consequences

**Positive**
- Uses the 128 GB instead of stranding it behind a single slot.
- Avoids the 8-minute cold-start tax for everything that fits — swap only what
  must swap.
- Builds on the safe primitives; the scary parts (confirmed-serving, liveness,
  fail-closed teardown) are done and reused per member.
- Orthogonal to and composable with the "engine dimension" direction (llama.cpp
  as a second engine): a fleet member is engine-agnostic; no dependency ordering
  between the two efforts.

**Costs / risks (be clear-eyed)**
- **Reconcile becomes set-valued.** The fail-closed teardown must become
  fail-closed *per member without cross-contamination* — a stuck `B` must never
  evict a healthy `A`. This is the bulk of the work and wants the same
  codex-loop / adversarial treatment the P0 deletion paths got.
- **Co-residency is planned + coarse-evict, NOT elastic.** vLLM's
  `gpu-memory-utilization` is fixed at launch; you cannot shrink a running
  instance to make room. "Make room for a big one" therefore means tearing a
  resident member *down whole* (dropping its in-flight requests → needs a
  drain/grace step), not resizing. Do not market this as shared/elastic memory.
- **New failure modes:** a member that under-reserves KV and OOMs under load;
  port exhaustion / collisions in the `:800x` map; a partial fleet `up` (some
  members serving, some not) — the atomic-set semantics must define this.
- **Budget estimation is only as good as the inputs.** Weight size and KV
  headroom are estimates; too tight → OOM mid-serving, too loose → fit fewer
  models. The reserve absorbs error, at the cost of density.

## Alternatives considered

- **Swap-on-demand (Ollama-style).** Rejected: the ~8-minute cold start makes
  per-switch swapping the dominant cost; co-residency is strictly better for
  anything that fits.
- **Status quo (single-slot replace).** Rejected: strands ~2/3 of the pool on a
  box sized for concurrency.
- **Elastic per-instance memory sharing.** Infeasible: `gpu-memory-utilization`
  is immutable post-launch; there is no supported shrink.
- **Manual multi-`up` on different ports, no budgeter.** Rejected as the product
  answer: it works mechanically today (ports are parameterized) but has no memory
  budget (OOM footgun), no atomic set semantics, and no eviction policy. The
  budgeter + set-reconcile is precisely what turns the raw capability into a safe
  feature.

## Scope

**In (phase 1, MVP):** the resident-set concept; set-valued reconcile for
status/recover; the static budgeter with fail-closed admission; per-member port
allocation; `gpu-memory-utilization` exposure; `fleet up`/`down`/`status`.

**Out (phase 2+):** dynamic evict-to-fit (LRU/priority selection + request
drain); autoscaling; cross-host fleets; elastic resizing (blocked upstream).

## Open questions

- Fleet manifest shape — a new file, or an extension of the existing serve
  manifest set?
- `pool_reserve` on unified memory: a fixed GB, or a fraction? Measured how?
- Partial-`up` policy: all-or-nothing, or best-effort-with-report?
- Does `llm-tidy`'s eviction interlock extend to fleet members (it already gates
  prune on `llm-serve liveness`)?
