# ADR 0002 — Single-GPU lease controller

**Status:** Proposed — *considered, not scheduled.* Captured so the reasoning
isn't re-derived when it comes up again.
**Created:** 2026-07-07
**Component:** new control plane, sits above `llm-serve` (ADR 0001 reconciler)
**Relates:** [ADR 0001](0001-llm-serve-resident-set-co-residency.md) is the
*mechanism* (co-residency + reconcile); this is the *control plane* that decides
what the desired set should be.

---

## Context

ADR 0001 lets multiple models co-reside on the one Spark GPU and reconciles a
desired set — but the desired set has to be *decided*, and the consumers are
distributed: remote tools and jobs need to declare "I need model X" and get a
fast yes/no. Not block (an 8-minute cold start means nobody can wait
synchronously), not silently thrash swaps.

The naive form spirals exactly as feared: a scheduler that *must* satisfy every
request has to figure out who/what/when/where to evict — and eviction implies
grace, grace implies drain, drain implies the API understanding request
lifecycles, and it never bottoms out. The GPU is a single scarce resource; the
honest model is not "always serve" but **"grant a slot or say no."**

Facts that shape it:
- **8-minute cold start** → model changes are rare, slow, deliberate. That is the
  wrong workload for a low-latency / consensus control plane. Admission decisions
  are fast; serving-readiness is slow and async.
- **We build the requesting tools.** Client-side cooperation (declare a tier,
  hold/renew a lease, back off on reject) is available — policy does not have to
  be inferred by the scheduler.
- **A second compute tier exists:** Ollama on Strix Halo. A caller that can't get
  a Spark lease is not stuck — it retries with backoff, or degrades to the
  smaller, always-available path.

## Decision

Build a **single-GPU lease controller**: a small, single-host, stateful admission
scheduler that governs which models occupy the Spark's memory budget, driven by
leases. The keystone primitives:

- **Leases, not bare requests.** A client reserves model X for a TTL (default
  ~1 h), renews to hold, and lets it drop when idle. This is what keeps the whole
  thing tractable: it turns "which to evict" from a scheduling solver into
  `reap-expired → honor-tier → reject`, and a vanished client self-cleans (its
  lease lapses, its budget frees).
- **First-class rejection.** When nothing is expired and nothing lower-tier
  yields, respond `422 / GOAWAY` with `Retry-After` set to the soonest-expiring
  lease — the reject tells the caller *exactly* when to come back. Rejection being
  cheap is what lets the admission policy stay dumb.
- **Async admission** (forced by the cold start):
  - resident already → `200 {endpoint}`
  - admitted, bringing up → `202 {reservation-id, provisioning}` (poll / callback)
  - can't fit → `422 {reason: capacity, retry-after}`
- **Tiers are declared, not inferred.** A client-side registry (TOML/YAML)
  assigns each requesting tool a tier/priority. The controller *honors* declared
  tiers; it does not do clever bin-packing. Policy lives in the registry, at the
  edge.
- **Decide/execute split.** The controller *decides* admission (reservation
  table + budget + leases). The ADR-0001 fleet reconciler *executes* bring-up /
  drain / teardown and reports "member serving," which flips the reservation to
  ready. Two composed state machines — the reservation lifecycle above
  (`Requested → Provisioning → Serving → Expiring → Released | Rejected`) over the
  member lifecycle that already exists in `llm-serve` (`Operation.Phase`,
  replace-with-recovery). Composition, not a state-machine framework.
- **The no-lease fallback is part of the contract.** A caller that can't hold a
  Spark lease either (a) retries with graceful backoff (it needs the big model),
  or (b) degrades to Ollama on Strix Halo (it can run smaller). The controller
  gates the *scarce premium slot*; it is not the only path. Some jobs will, by
  design, never run on the Spark under contention — an accepted outcome, and the
  reason the fallback is load-bearing rather than optional.

## Consequences

**Positive**
- The GPU becomes a governed, shareable resource with legible admission instead
  of a single-slot free-for-all.
- Eviction is a *sort* (expired, then tier), not a solver — small and auditable.
- Backpressure (422 + Retry-After) makes callers coordinate themselves; the
  controller stays simple.
- Sits directly on ADR 0001 — no new runtime mechanism, just a decision layer.

**Costs / risks (clear-eyed)**
- **This is a resident, stateful daemon** — a real step up from `llm-serve`'s
  stateless reconciler. Justified by the demand-driven requirement, but it adds an
  always-on failure surface: the lease table is now authoritative state that must
  survive restarts.
- **Keep it single-host.** One Spark → one controller. No leader election, no
  consensus, no split-brain. The moment a second serving box appears this becomes
  a genuinely harder distributed problem — resist until forced (that would be a
  future ADR, not this one).
- **Don't preempt an in-flight bring-up.** A provisioning member holds its budget
  for the full ~8 min; a higher-tier request waits or gets 422. Preempting wasted
  cold-starts is a v2 footgun.
- **Lease granularity is session/job, not per-request** — at 8 min/swap,
  per-inference reservations thrash. The API shape must reward holding, not
  re-requesting.
- **Starvation is possible by design** — a low-tier job under sustained
  contention may never get the Spark. This is intended; it is why the Ollama/Strix
  fallback is required, not optional.

## Alternatives considered

- **Passive service discovery** (publish fleet state, consumers watch).
  Insufficient: consumers here *request capacity* and need admit/reject, not just
  a read of current state.
- **Synchronous "serve model X" call.** Untenable: 8-minute cold start; nobody
  blocks. Async reservation is forced.
- **Scheduler decides eviction cleverly (bin-pack / preempt).** Rejected — that is
  the complexity spiral. Declarative tiers + leases + first-class reject keep the
  decision a sort.
- **Distributed / multi-host control plane (consensus).** Rejected for now: one
  serving box; premature. Revisit only when a second box is real.
- **No controller — clients coordinate ad hoc.** Rejected: no budget authority →
  OOM footgun; no fair admission; no backpressure.

## Scope

**In** (if/when actioned): the lease/admission API (reserve / renew / release;
200 / 202 / 422 with Retry-After); the reservation + budget + lease table
(durable, single-host); the client-side tier-registry contract; integration with
the ADR-0001 reconciler as the executor; the documented no-lease fallback to
retry / Ollama-on-Strix.

**Out:** distributed / multi-host scheduling; preemption of in-flight bring-ups;
per-request granularity; autoscaling beyond lease-driven admission.

## Open questions

- Where does the lease table live — an embedded KV on the box, or a file? Durable
  vs simple.
- Is the controller a new binary, or an `llm-serve` mode? (Leans new — it is
  stateful/resident, unlike today's stateless `llm-serve`.)
- Tier registry: purely client-honored, or also enforced server-side so a client
  can't over-claim its tier?
- When the soonest lease expiry is far out, does the controller offer a
  callback/queue, or is `Retry-After` + polling enough?
- Does the ADR-0001 router (single endpoint → `:800x`) become the natural home
  for the admission edge, since it already sits in the request path?
