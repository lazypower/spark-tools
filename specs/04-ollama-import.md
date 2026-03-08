# hfetch ollama-import — Load GGUF Models into Ollama

**Status:** Draft
**Created:** 2026-03-06
**Component:** `hfetch`
**Language:** Go (pure — zero cgo)

---

## 1. Problem Statement

Users download GGUF models via hfetch and want to use them with Ollama. Today this requires manually writing a Modelfile, knowing the local file path, and running `ollama create`. Split GGUF models add another layer of friction — Ollama doesn't support sharded GGUFs, so users must also locate and run `llama-gguf-split --merge` before import.

We need a single command that resolves a model from the hfetch registry, handles shard merging in pure Go if needed, generates a minimal Modelfile, and calls `ollama create`.

## 2. Goals

1. **One command.** `hfetch ollama-import <model-ref>` takes a model from "downloaded via hfetch" to "available in ollama" with no intermediate steps.
2. **Pure Go shard merge.** Split GGUFs are merged without shelling out to llama.cpp tools. Keeps hfetch's zero-external-dependency guarantee.
3. **Minimal Modelfile.** Just `FROM /path/to/model.gguf`. Ollama reads GGUF metadata natively — no need to duplicate template, parameters, or system prompt.
4. **Predictable naming.** The Ollama model name defaults to something sensible (e.g., `llama-3-8b:Q4_K_M`) and can be overridden.

## 3. Non-Goals

- Rich Modelfile generation (TEMPLATE, PARAMETER, SYSTEM directives). Ollama extracts these from GGUF metadata.
- Managing Ollama models after import (list, rm, update). Users have `ollama` for that.
- Downloading models. The model must already exist in the hfetch registry. Use `hfetch pull` first.
- Supporting non-GGUF formats.

## 4. CLI Interface

```
hfetch ollama-import <model-ref> [flags]

Arguments:
  model-ref    Model identifier in the hfetch registry.
               Formats: "org/model", "org/model:quant", or a local file path.

Flags:
  --name, -n   Override the Ollama model name (default: auto-derived)
  --dry-run    Print the Modelfile and ollama create command without executing
  --keep       Keep the merged GGUF file after import (only relevant for split models)
```

### Examples

```sh
# Import a single-file model
hfetch ollama-import bartowski/Llama-3-8B-GGUF:Q4_K_M

# Import with a custom Ollama name
hfetch ollama-import bartowski/Llama-3-8B-GGUF:Q4_K_M --name llama3

# Preview what would happen
hfetch ollama-import bartowski/Llama-3-8B-GGUF:Q4_K_M --dry-run

# Import a split model (shards merged automatically)
hfetch ollama-import mradermacher/Llama-3-70B-GGUF:Q4_K_M
```

## 5. Model Resolution

Resolution uses the existing hfetch registry and file resolution:

1. **Parse model-ref.** Split into model ID and optional quant suffix (`:Q4_K_M`).
2. **Look up in registry.** Find the `LocalModel` entry. Fail if not found (suggest `hfetch pull`).
3. **Select files.** If quant specified, filter to matching files. If only one quant exists, use it. If ambiguous, error with available options.
4. **Detect split.** If the selected quant has multiple shard files (e.g., `*-00001-of-00003.gguf`), route through the merge path.

## 6. GGUF Shard Merge

### 6.1 Background

`gguf-split` produces shards where each is a valid GGUF file:
- **Shard 0** contains all model metadata KVs plus split-tracking keys (`split.no`, `split.count`, `split.tensors.count`).
- **Each shard** has its own tensor info entries and tensor data for its subset of tensors.
- Tensors are never split across shards — each tensor lives entirely in one shard.

### 6.2 Merge Algorithm

```
MergeShards(shardPaths []string, outputPath string) error:

1. Open all shards. Parse headers to collect:
   - Metadata KVs from shard 0 (the authoritative source)
   - Tensor info entries from all shards (name, dimensions, type, offset)
   - Alignment value from shard 0 (general.alignment, default 32)

2. Build unified output:
   a. Strip split-specific KVs: split.no, split.count, split.tensors.count
   b. Sum tensor counts across all shards → new header tensor_count
   c. Recalculate tensor data offsets sequentially (each tensor aligned)

3. Write output file:
   a. GGUF header: magic, version, tensor_count, kv_count
   b. All metadata KVs from shard 0 (minus split.* keys)
   c. All tensor info entries (with recalculated offsets)
   d. Pad to alignment boundary
   e. Copy tensor data from each shard sequentially, respecting alignment

4. Verify output: parse the merged file header to confirm valid GGUF.
```

### 6.3 Implementation Location

New file: `pkg/hfetch/gguf/merge.go`

The merge operates at the binary level — it needs to read/write raw GGUF structures, not just the parsed metadata our current `Parse()` returns. This requires extending the gguf package with:

- **Tensor info parsing.** Read tensor name, n_dims, dimensions, type from each shard header (currently skipped by `Parse()`).
- **Raw KV preservation.** Store the raw binary representation of each KV pair so we can write them back verbatim (avoids lossy round-trips through Go types).
- **GGUF writer.** Write a valid GGUF header + tensor info + tensor data.

### 6.4 Merged File Location

Merged files are written alongside the shards in the model directory:
```
$HFETCH_DATA_DIR/models/org--model/
├── model-Q4_K_M-00001-of-00003.gguf   # shard (kept)
├── model-Q4_K_M-00002-of-00003.gguf   # shard (kept)
├── model-Q4_K_M-00003-of-00003.gguf   # shard (kept)
└── model-Q4_K_M-merged.gguf           # merged output
```

The merged file is registered in the manifest as an additional file entry. Shards are preserved by default (they're the canonical download artifacts). `--keep` is the default; without it, the merged file is treated as ephemeral and may be cleaned up by `hfetch gc`.

## 7. Ollama Integration

### 7.1 Modelfile Generation

Minimal — just the FROM directive:

```
FROM /absolute/path/to/model.gguf
```

Written to a temp file, cleaned up after `ollama create` completes.

### 7.2 Ollama Model Naming

Default name derivation from the model ID and quant:

```
org/Model-Name-GGUF:Q4_K_M  →  model-name:Q4_K_M
```

Rules:
1. Drop the org prefix
2. Strip `-GGUF` / `-gguf` suffix from the model name
3. Lowercase, keep hyphens
4. Quant becomes the Ollama tag (after the colon)

Override with `--name` for full control.

### 7.3 Execution

```go
exec.Command("ollama", "create", modelName, "-f", modelfilePath)
```

- Check `ollama` is on PATH before starting any work. Error early with guidance if not found.
- Stream stdout/stderr to the terminal so the user sees Ollama's progress.
- Non-zero exit from `ollama create` propagates as an error.

## 8. Package Structure

```
pkg/hfetch/gguf/
├── merge.go          # MergeShards() — pure Go GGUF shard merger
├── merge_test.go     # Tests with synthetic split GGUF fixtures
├── writer.go         # GGUF binary writer (header, KVs, tensor info, data)
├── writer_test.go
├── parser.go         # (existing) extended with tensor info parsing
├── types.go          # (existing) extended with TensorInfo type
├── filter.go         # (existing) unchanged
└── fit.go            # (existing) unchanged

cmd/hfetch/
└── ollama.go         # ollama-import subcommand wiring
```

## 9. Error Handling

| Condition | Error | Guidance |
|-----------|-------|----------|
| Model not in registry | `model "org/model" not found locally` | `Run: hfetch pull org/model` |
| Ambiguous quant | `multiple quantizations available` | List available quants with sizes |
| `ollama` not on PATH | `ollama not found` | `Install: https://ollama.com/download` |
| `ollama create` fails | Pass through Ollama's error | — |
| Corrupt shard during merge | `shard N: invalid GGUF header` | `Re-download: hfetch pull --force org/model` |
| Disk space insufficient | `not enough space for merge` | Show required vs available space |

## 10. Future Considerations

- `hfetch ollama list` / `hfetch ollama rm` — if there's demand for managing the Ollama side too.
- Skip merge if Ollama adds native split GGUF support (check `split.no` metadata key presence; if Ollama handles it, just point the Modelfile at shard 0).
- `--quantize` flag to have Ollama re-quantize during import.
