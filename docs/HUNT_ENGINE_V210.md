# Hunt engine v2.10 — max-noticeable depth (FounderB)

Extends rc17.2 **DeepHavocV28** + FounderB **v2.9** soft weights.

## What’s new

| Piece | Effect |
|-------|--------|
| `applyDeepV210Burst` | After deep-v28: multi-token dict burst, widen/narrow, interleave, rare nibble, CmpLog splice, overlong UTF-8, 3-way splice |
| Feature flags | `deep_v210_burst`, `ubsan_frame_keys` |
| Hunt local reports | Stack keys parse `.h:` / `runtime error:` / `#0`; families via `FindingFamily` (honesty 2.0) |

## Honesty

- Still **deterministic** (stage+salt only).
- **Default new Hunt** enables deep-v28 only (`EnableDeepHavocV28`).
- **v2.10 burst** is opt-in (`havoc_deep_v210=true` or profile `v210`/`max`) — measured unique can *dip* vs v28 at fixed sample/maxLen due to length saturation; use when you want harder smash, not raw unique%.
- Not GPU ASAN. Issue #13 **E** is Dig mutant *generation* (`internal/gpudig`) → CPU WASM/ASAN eval.

## Gates

```bash
go test ./internal/fuzzengine/ -count=1 -timeout 180s
go test ./internal/gpudig/ ./internal/hunt/ -run 'Failclosed|Refuse|GenerateMutants' -count=1
bash scripts/tests/fuzz_engine_local_stress.sh
```
