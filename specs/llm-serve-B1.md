# llm-serve (B) v2 — slice B1: launch + supervise (drive-the-driver) — PLAN v3

Status: revised after Mode-A round 2 (codex). Absorbs round-2's 4 P1s + the
livelock note. No code yet.
Builds on: v1-A (emit-only contract engine, shipped + operator-accepted on the Spark).

## Settled decisions (operator)
- **drive-the-driver:** emit the v1 spec, apply via `docker compose up -d`,
  reconcile desired vs actual. No native runtime ownership.
- **no native watchdog:** wedge detection preserved by emitting the existing
  watchdog sidecar; not reimplemented, not dropped.
- **slice order:** B1 → B2 liveness ledger → B3 tidy interlock → B4 probes (v2.1).

## Product promise
Own a serving instance's lifecycle — bring it up to *confirmed* serving, keep
desired==actual, tear it down cleanly — without reimplementing the runtime and
**without ever reporting a state it cannot confirm.**

## State = ONE atomic manifest per instance (R2-P1.2 fix: no torn two-record write)
A single per-instance file, written ONLY via temp+rename (one atomic swap), so two
sub-records can never disagree across a crash. Sections:
- **desired** — pure intent + stable identity, the ONLY authority for "what should
  run": `{ name (THE identity), served_name, model_id, model_revision, model_dir,
  contract_key, spec_path, spec_hash, target_fingerprint, project_name }`. No
  readiness/liveness — those are derived, never stored.
- **candidate** (present only during a replace) — a second desired block being
  brought up; promoted to `desired` atomically on success, discarded on failure.
- **operation** — recovery state for an in-flight mutation only:
  `phase ∈ {starting, replacing, stopping, cleanup_required}`. Terminal states
  (`ready`, `stopped`) are NOT stored; readiness is derived, `stopped` = no manifest.

**Readiness/serving is ALWAYS derived from the runtime, never read from the manifest.**

## Identity & confirmation: labels cover the FULL desired identity (R2-P1.3 fix)
- **One canonical identity = `name`**; `project_name`, labels, served name derive from it.
- **Emitted stack carries immutable labels on every service covering every
  distinguishing desired field** — `managed-by=llm-serve, instance=<name>,
  contract-key=<key>, spec-hash=<hash>, served-name=<served>, model-id=<id>,
  model-revision=<rev>, target-fingerprint=<fp>`. Rule: if a field distinguishes
  one desired record from another, it is in the labels, or reconcile cannot prove
  the runtime IS the desired instance. (B1 extends v1 emit to stamp these.)
  `project_name` derives deterministically from `name` (no own label needed);
  `spec_path` is local bookkeeping (not runtime identity); `model_dir` is covered
  derivatively — it determines the emitted `--model` container path, which is in the
  flags `spec_hash` hashes, so a meaningful `model_dir` change changes `spec_hash`.
- **Reconcile verifies labels BEFORE readiness.** Any mismatch (stale container,
  old artifact/revision, project-name collision) ⇒ **conflict ⇒ not-serving, refuse
  adoption**. Don't fight a hand-launched/stale stack; report it.
- **"Serving" is a derived predicate requiring ALL of:**
  1. required services running with labels matching the relevant desired/candidate
     block — **main engine AND watchdog sidecar** (missing watchdog ⇒ **fail closed**);
  2. `/health` == 200;
  3. a **warmup completion against the EXACT served_model_name** — non-empty
     generation, no API error (minimum "actually serving THIS model" evidence;
     distinct from B4 conformance). Bounded; a slow boot stays `starting` until the
     boot-timeout, only THEN failed (no premature destructive cleanup of a model
     that was still coming up).
  UNKNOWN/unreachable ⇒ not-serving (fail closed).

## Crash-safe ordering (R3): manifest must exist before any runtime resource
**Invariant: a manifest exists before `compose` can create a stack** — an orphan
spec is harmless, an orphan stack with no manifest is not. Ordering:
`write spec (atomic temp+rename)` → `write manifest{desired, operation:starting}
(atomic)` → `compose up -d` → confirm predicate → `atomic clear operation`.

## "Stopped" vs "drift" (R3): a desired with no stack is NOT stopped
- `stopped` = **no manifest**. A manifest with `desired`, no `operation`, and **no
  running stack** is **drift / runtime-absent**, never "stopped."
- Pure reads report it `not-serving` / `recovery-pending` (never `stopped`).
- Mutating recovery resolves it: relaunch the desired stack, or set a
  recovery-pending operation phase — its existence as desired means it *should* run.

## Transactional bring-up & fail-closed cleanup (R1-P1.1)
- Bring-up follows the crash-safe ordering above → poll the serving predicate within
  boot-timeout → on success atomically clear `operation` (desired remains).
- On failure/timeout: `compose down`; **clear the manifest ONLY on CONFIRMED runtime
  absence.** If teardown can't prove the stack is gone, set `operation:cleanup_required`
  (keep spec/project handles); `status` reports UNKNOWN/not-serving. Never erase the
  only recovery handle on unconfirmed cleanup.

## Replacement: explicit current+candidate, single port ⇒ destructive but recoverable (R2-P1.1)
One :8000 ⇒ no overlap, so replace is destructive — but the OLD spec is preserved so
it can be restored. With manifest{desired=current}, `up` of a changed contract:
1. resolve+emit+**validate the candidate spec OFFLINE first**;
2. atomically write `candidate` + `operation:replacing` (current still recorded);
3. `compose down` current → `compose up -d` candidate;
4. poll the serving predicate **against the candidate's labels**;
5. on success: atomically **promote candidate→desired**, clear operation;
6. on failure: discard candidate, **best-effort restore current** from its preserved
   spec. **Never clear back to a normal `desired=current` state unless current's OWN
   serving predicate is re-confirmed** — otherwise leave `operation:cleanup_required`
   / recovery-pending (don't falsely report current restored). **Never claim the
   candidate serving until its predicate holds.** Current's intent/spec is never lost.
- (Zero-downtime blue-green needs a second port = topology/co-residency — deferred.)

## Concurrency, pure reads, locked recovery (R1-P1.6 + R2-P1.4)
- Manifest + spec writes are **atomic** (temp+rename of the single manifest).
- **Mutating ops** (`up`/`down`/replace/recover) hold a **host file lock** (serialized).
- **Reads (`status`/`ps`) are PURE** — they take a consistent snapshot and report what
  they observe (serving / not-serving / conflict / unknown / recovery-pending). They
  **never mutate**, so they can't race a mutation or tear down the wrong instance.
- **Startup recovery is a MUTATING operation under the host lock.** It runs at the
  start of every mutating command (and an explicit `recover`), reconciling ALL
  manifests against the runtime **regardless of operation phase** (a desired with no
  active operation but a live/half stack is still reconciled — truth is the runtime).

## Operator escape hatch (R2 livelock note, R3 tightening)
`llm-serve forget <name> --force` — operator-confirmed abandon of a manifest stuck in
`cleanup_required` when the runtime is permanently gone, so a dead Docker can't wedge
future `up` forever. It **prefers confirmed runtime absence**; if absence cannot be
confirmed it proceeds only with an explicit "I accept a possibly-orphaned runtime"
acknowledgement, stated loudly. Operator-initiated only.

## B2 seam
The `desired` block is a strict subset B2 absorbs without migration; B2 adds the
shared **liveness ledger** as appendable observations ALONGSIDE the manifest, never
rewriting B1 intent semantics. B1 stores no liveness; B2 owns it.

## CLI surface
- `llm-serve up <model-dir> --name … --cap … --image … --mount …` — resolve→emit(+labels)
  →apply→wait for serving predicate→report. Fail-closed on timeout. Changed contract ⇒
  named destructive replace.
- `llm-serve down <name>` — confirmed teardown; `cleanup_required` on unconfirmed.
- `llm-serve status [name]` / `ps` — pure consistent-snapshot reconcile; never false-ready.
- `llm-serve recover` — run recovery under lock (also auto-run by mutating ops).
- `llm-serve forget <name> --force` — operator abandon of an unrecoverable manifest.

## Definition of done
1. `up` reaches the confirmed serving predicate (full-identity labels + health +
   served-model warmup + watchdog) or fails closed with confirmed cleanup — no orphan
   stack, no orphan manifest, no false-ready.
2. State is one atomic manifest; readiness always derived; no torn write, no competing
   authority.
3. Unconfirmed cleanup keeps a recovery handle; `forget --force` is the only abandon.
4. Replace preserves current until candidate is confirmed; failure restores/best-effort,
   never claims unconfirmed serving.
5. Reads are pure; mutations + recovery are locked & serialized; no races.
6. Labels cover the full desired identity; mismatch ⇒ conflict, not adoption.
7. Watchdog absent ⇒ fail closed.
8. `desired` block is a forward-compatible subset of the B2 ledger (no migration).
9. Seam tests green; Mode-A clean; Mode-B on the diff clean.
