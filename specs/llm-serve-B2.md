# llm-serve (B) v2 — slice B2: runtime-liveness (eviction protection) — PLAN v2

Status: DRAFT for Mode-A re-audit. SIMPLIFIED after the operator challenged the
cron/ledger as over-engineered (correctly). No code yet.
Builds on: B1 (drive-the-driver lifecycle; atomic per-instance manifest of pure
desired intent; reconcile derives serving live from the runtime).

## The simplification (why there is NO ledger, NO cron, NO lease)
The eviction question is "is anything using this artifact?" — answerable LIVE by
`docker ps`/`inspect` filtered to llm-serve's managed labels: cheap (one call
lists everything), and it needs none of B1's serving-predicate machinery (no
health, no warmup — eviction-protection is a WEAKER question than "ready to
serve"). A live query has no staleness, so a cached ledger + heartbeat + TTL would
only re-introduce a staleness problem to then guard against. Dropped entirely.

Liveness is therefore DERIVED, never stored — the same principle B1 already holds
for "serving". B2 adds no new persistence; it reuses the manifests B1 already
writes plus a live runtime query.

## Product promise
Answer **"is this artifact protected from eviction?"** for other tools (B3's
llm-tidy interlock) — fail-closed, never reporting evictable when anything live or
intended uses the artifact — by a cheap live query, no daemon, no cache.

## The verdict: protected = (running managed containers) ∪ (desired manifests)
An artifact is **PROTECTED** if its canonical key is in the union of:
1. **Live:** host artifact paths of currently-running llm-serve-managed containers
   (`docker ps`/inspect by `managed-by=llm-serve`). The host path is read from an
   explicit **`artifact-host-path` label** stamped at emit (see below) — NOT
   derived from the `--model` command arg, which is the CONTAINER path
   (`/models/hf/...`), not the host path the artifact lives at.
2. **Intended:** artifacts named by any existing B1 **desired manifest** (its
   `ModelDir`) — intent-to-serve protects the weights even if the container is
   momentarily down (crash/restart/cold boot), and B1 removes a manifest ONLY on
   confirmedDown, so a draining/unconfirmed teardown stays protected for free.

The two halves cover each other: the manifest half protects normal instances even
when momentarily not running; the live half protects an instance whose manifest
was removed while its stack survives (e.g. `forget --accept-orphan`).

An artifact is **EVICTABLE** only when it is in NEITHER set: no running managed
container AND no desired manifest references it. Absence-of-a-running-container
alone is NOT evictable — the manifest carries the intent.

This makes the two Mode-A-round-1 cardinal-sin paths impossible by construction:
- cold-start / "no record" → a manifest exists ⇒ protected (no cron to miss);
- `down` fail-open → manifest persists until confirmedDown ⇒ protected while draining.

## The live container → host artifact path mapping (Mode-A round-2 P1)
A managed container reports its model as the CONTAINER path; the artifact on disk
is the HOST path. B2 must map running container → host artifact path RELIABLY, or
it would protect the wrong key:
- **emit stamps an `artifact-host-path` label** = the canonical host artifact dir
  (`facts.ModelPath`, already absolute). The live query reads this label per
  managed container — the host path, self-reported, no command-arg guessing.
- The runtime gains a **list-all-managed** query (`docker ps`/inspect by
  `managed-by=llm-serve` across ALL projects, returning each container's labels) —
  B1's Inspect is per-project; B2 spans every instance on the host.
- **Fail-closed if underivable:** a running managed container whose
  `artifact-host-path` label is missing/unparseable means B2 cannot know which
  artifact it serves — so it cannot safely call ANY artifact evictable. That query
  returns **everything protected** (unknown ⇒ protected), never silently skips the
  mystery container. (This only fires on corruption/a bug — every llm-serve emit
  stamps the label.)

## Canonical artifact identity — ONE authority
B3 evicts by on-disk artifact path; both the live label and the manifest ModelDir
are host paths. A raw compare is unsafe (symlink, relative/abs, trailing slash,
case). B2 owns the canonicalization and the membership test:
- canonical key = `EvalSymlinks` + `Abs` + `Clean` of the artifact dir.
- expose **`IsProtected(artifactPath) bool`** — it canonicalizes the ARGUMENT the
  same way and tests membership; consumers never compare raw paths themselves.
- **`ProtectedArtifacts() → set of canonical keys`** — the union above.
- a path that cannot be canonicalized (missing/unreadable) is treated as
  **protected** (fail-closed) — never silently dropped.

## Fail-closed on the query itself
- `docker` unreachable / inspect errors → the live set is **unknown** ⇒ every
  artifact is treated **protected** for that query (never evictable on an error).
- The manifest set is local (always readable); a manifest read error ⇒ protected.

## Read API + CLI
- Go: `IsProtected(path) bool`, `ProtectedArtifacts() []ArtifactKey`,
  `Liveness(name) → {running bool, hasManifest bool, protected bool, reason}`.
- CLI: `llm-serve liveness [name]` (human + machine-readable), driven by the same
  query. Reads never mutate and never drive a serving probe (just ps/inspect).

## Scope
IN: the list-all-managed runtime query, the protected-set union over running
containers + desired manifests, the canonical artifact-key authority, the
`IsProtected`/`ProtectedArtifacts` API + CLI, fail-closed semantics.

OUT (deferred / explicitly not B2):
- A persisted liveness ledger / heartbeat / lease / TTL / `refresh` timer —
  REMOVED as over-engineered (this revision).
- The **llm-tidy interlock** (B3): B2 exposes the authority; wiring tidy to
  consult it before evicting is B3.
- Capability-proven liveness (B4): B2 liveness = a running managed container or a
  desired manifest, not a serving/health proof.
- Protecting NON-llm-serve-managed containers (hand-launched / legacy run.sh): out
  of scope for B2's authority; B3/tidy may layer a broader "any container using
  this path" rule, but B2 answers only for what llm-serve manages.
- Cross-host / co-residency beyond what the instance-keyed query already gives.
- Filesystem omnipotence; operator hand-deleting manifests or weights.

## B3 seam (drawn, not built)
B3's interlock calls `IsProtected(candidatePath)` before evicting and skips a true
result; an errored/unknown query ⇒ protected by construction. Whether llm-tidy
imports the llm-serve liveness package or shells to `llm-serve liveness` is a B3
decision — B2 just exposes a stable, fail-closed query over one canonical key.

## Definition of done (B2)
1. `IsProtected(path)` returns protected for any artifact served by a running
   managed container OR named by a desired manifest; evictable only when neither.
2. Evictable is never inferred from absence-of-a-running-container alone (manifest
   intent protects); never published before B1's confirmedDown.
3. One canonical artifact-key authority; consumers never compare raw paths.
4. Fail-closed: docker-unreachable, uncanonicalizable path, or read error ⇒
   protected.
5. No new persistence, no daemon, no timer — liveness is derived live.
6. Seam tests green (protected-set vs a fake runtime + real manifests); Mode-A
   clean; Mode-B on the diff clean.

## Open questions for the audit
- The protected-by-manifest rule means a `cleanup_required`/failed instance's
  artifact stays protected until the operator `forget`s it. Correct (fail-closed)
  or too sticky? (Leaning correct: protection follows the operator-controlled
  manifest lifecycle.)
- Should B2 also protect against a hand-launched (non-managed) container using the
  path, or is that explicitly B3/tidy's broader policy? (Plan says out of scope.)
- Co-residency: many managed containers may share one artifact path — the union
  handles it, but confirm the canonical-key dedup is the right grain.
