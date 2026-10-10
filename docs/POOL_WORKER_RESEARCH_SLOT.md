# Worker-local research slot (Stage D)

Optional hybrid path: a **short local libFuzzer persist window** while a Hunt lease is still held, then submit **corpus deltas + crashes only** (not full research-exec replay of the shard). Crashes are ASAN-replayed on the coordinator before any finding is recorded.

## Status

| Piece | State |
|-------|--------|
| Flag `HACKME_WORKER_RESEARCH_SLOT` | **Landed, default OFF** |
| Local LF persist window | **Wired** (`hunt.RunPersistentLibFuzzerSession`) |
| Corpus-delta submit API | **Wired** `POST /api/fuzz/work/corpus_delta` |
| Crash ASAN replay | **Wired** (fail closed if `HACKME_POOL_HUNT_REPLAY=0`) |
| `HACKME_POOL_SEED_FROM_RESEARCH` | **Unchanged, default OFF** (Dig seed feed; separate gate) |

## Intended flow

1. Worker finishes a Hunt shard (lease still held).
2. If `HACKME_WORKER_RESEARCH_SLOT=1`, run LF for `HACKME_WORKER_RESEARCH_WINDOW_SEC` (default 30s, cap 120s) against `reports/oss-cve-libfuzzer/<target>/corpus`.
3. Collect **new** corpus files + crash artifacts (cap `HACKME_WORKER_RESEARCH_MAX_DELTA`).
4. POST delta to coordinator (worker auth + **active lease** bind); namespace must be `research:<upstream_target_id>`.
5. Coordinator merges non-crash seeds into research namespace + Hunt campaign corpus; crash bytes are ASAN-replayed — forged crashes mint nothing.
6. Worker then submits the normal shard result (sampled / crash-first path unchanged).

## Enable safely (subset of miners)

```bash
# On selected Hunt-capable miners only (keep fleet default OFF):
export HACKME_WORKER_RESEARCH_SLOT=1
export HACKME_WORKER_RESEARCH_WINDOW_SEC=30   # 5–120
export HACKME_WORKER_RESEARCH_MAX_DELTA=32    # 1–256
# Optional Dig research-only campaigns (config research_slot_ok=true + target override):
# export HACKME_WORKER_RESEARCH_SLOT_DIG=1
# export HACKME_WORKER_RESEARCH_TARGET=jsmn
```

Requires clang + libFuzzer harness build path used by Hunt LF import. Missing LF → slot skips; claim/submit continues.

## Security

- Default OFF on fleet.
- Worker token required; **active lease** owner must match `worker_id`.
- Namespace restricted to `research:<target>` — never `pack:*` / customer Dig namespaces.
- Namespace must match the Hunt campaign `upstream_target_id` (cross-campaign inject rejected).
- Crash artifacts never trusted without coordinator ASAN replay.
- Research path **does not** enqueue `settle_finding` / mint bounty escrow (shard submit + deferred native gates remain crash-first).
- Must not auto-enable `HACKME_POOL_SEED_FROM_RESEARCH`.
- Path-safe corpus dirs only (see `internal/fuzzingcli/research_seed_feed.go`).

## Env knobs

| Env | Default | Meaning |
|-----|---------|---------|
| `HACKME_WORKER_RESEARCH_SLOT` | OFF | Enable local LF window + corpus_delta |
| `HACKME_WORKER_RESEARCH_WINDOW_SEC` | 30 | LF wall seconds (5–120) |
| `HACKME_WORKER_RESEARCH_MAX_DELTA` | 32 | Max new corpus files to upload |
| `HACKME_WORKER_RESEARCH_SLOT_DIG` | OFF | Also run on Dig when campaign allows |
| `HACKME_WORKER_RESEARCH_TARGET` | (claim) | Override target id when claim has none |
| `HACKME_POOL_SEED_FROM_RESEARCH` | OFF | Separate Dig seed feed — do not couple |

## Related

- [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md) — claim/submit anticheat
- [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) — honest LF vs Hunt depth
