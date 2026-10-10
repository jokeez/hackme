# Worker-local research slot (Stage D — design)

Optional hybrid path for Dig workers: a **short local libFuzzer persist window** between pool claims, then submit **corpus deltas + crashes only** (not full research exec replay on the coordinator).

## Status

| Piece | State |
|-------|--------|
| Flag `HACKME_WORKER_RESEARCH_SLOT` | **Landed, default OFF** |
| Stub `MaybeRunResearchSlot` | No-op / `stub_not_wired` when enabled |
| Local LF persist window | **Residual** — not wired |
| Corpus-delta submit API | **Residual** — not wired |
| `HACKME_POOL_SEED_FROM_RESEARCH` | **Unchanged, default OFF** (Dig seed feed; separate gate) |

## Intended flow (future)

1. Worker finishes a Dig claim/submit.
2. If `HACKME_WORKER_RESEARCH_SLOT=1` and campaign allows, run LF for `HACKME_WORKER_RESEARCH_WINDOW_SEC` (default 30s, cap 120s) against a local corpus dir.
3. Collect new corpus files + crash artifacts (cap `HACKME_WORKER_RESEARCH_MAX_DELTA`).
4. POST delta to coordinator (auth + lease/campaign bind); coordinator merges into guided corpus **after** hash checks — never trusts worker “found” without crash-path replay.

## Security

- Default OFF on fleet.
- Must not mint bounty from research-only signals without crash-first replay.
- Must not auto-enable `HACKME_POOL_SEED_FROM_RESEARCH`.
- Path-safe corpus dirs only (see `internal/fuzzingcli/research_seed_feed.go`).

## Related

- [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md) — claim/submit anticheat
- [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) — honest LF vs Hunt depth
