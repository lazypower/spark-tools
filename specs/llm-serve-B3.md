# llm-serve (B) v2 — slice B3: the llm-tidy eviction interlock

Status: built (light path). Consumes B2 liveness. Seam = CLI shell-out (operator
decision; a shared-contract-surface extraction is parked for the next refactor).

## What it is
Before llm-tidy deletes a model's files, it asks llm-serve's liveness authority
whether that path is protected (a running container uses it, or it's a desired
instance) and NEVER prunes a protected path. Fail-closed by construction.

## The seam (CLI shell-out)
- llm-serve gains `liveness --check`: reads candidate paths on stdin, prints the
  PROTECTED subset on stdout (the overlap is computed by llm-serve — one
  authority), and the unmanaged-container complaints on stderr.
- llm-tidy shells out to it (`pkg/llmtidy/interlock`), so llm-tidy depends only on
  llm-serve's stable CLI contract, not its internals.

## The gate (pkg/llmtidy/interlock)
`Apply(plan, checker)` partitions a prune plan:
- candidates with no on-disk path (**Ollama** — deleted via its own API, governed
  by Ollama's runtime) pass through unchecked;
- path-based candidates (**GGUF** now; vLLM when a backend is added) that liveness
  reports protected are BLOCKED;
- **fail-closed**: llm-serve present but undeterminable ⇒ block ALL path-based
  candidates;
- **inactive**: llm-serve absent (not deployed) ⇒ plan passes through (no
  llm-serve-served models to protect here);
- complaints (unmanaged containers holding a model store) are surfaced.
`llm-tidy prune --no-interlock` is the explicit operator bypass.

## Scope / honesty
- The interlock is a GUARANTEE + a coexistence COMPLAINT. It is mostly latent
  today: GGUF lives in the hfetch dir, vLLM models elsewhere, so they rarely
  overlap. Its teeth land when llm-tidy gains a **vLLM backend** (prune vLLM models
  "like any other backend") — a deliberate SEPARATE slice. B3 is forward-compatible
  for it (the gate is general/path-based).
- Ollama is out of the llm-serve interlock by nature (no path; its own runtime).

## Done
- llm-tidy prune never deletes an llm-serve-protected path; fail-closed; the
  unmanaged complaint is surfaced; `--no-interlock` bypass.
- Unit-tested (the filter); the shell-out + real liveness verified on the host.
- Mode-B (codex) on the diff clean.
