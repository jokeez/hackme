# Hunt pool power deepen — self-audit

Branch: `feat/hunt-pool-power-deepen`. No pull request.

Scope is the distributed Hunt lease (libFuzzer / AFL), not Dig WASM. Dig-only workers still advertise an empty `HuntHarnessExec` and are not given a harness default.

## What changed

- Pool walls that actually run: lite 180s, standard 180s, heavy 210s. Fallback when `engine_lease_sec` is unset is 90s (was 60s). Coordinator coverage lease cap is 480s so a 210s wall plus merge slack is not clipped at 360s. Byte Hunt stays capped at 360s. Dig exec/unit cap is unchanged.
- Shard iteration defaults: lite 96, standard 288, heavy 384 (cap 384). Coverage claims still set `exec_per_unit=1`; the wall is the depth that runs.
- `seed_byte_corpus` is reserved ahead of a fat guided snapshot (96 files). Claim wire trim is 96 items (above the rank cap of 64) and 256KiB. Seeds are written as content-addressed `claim-seed-<sha>.bin` before the libFuzzer wall. Generic `{}` boot starters are not planted when claim seeds were supplied. An inventory claim with zero seeds soft-skips as `inventory_seeds_missing` instead of submitting CLEAN on boot starters.
- Corpus rows accept `input_bytes_hex` and raw `input_bytes`. Raw strings are not hex-decoded (`HWIR` stays four bytes).
- Catalog targets get a real libFuzzer `-dict=` (profile text). An inventory `fuzz/*.dict` still wins. Workers fall back to the profile dict when the claim omits `libfuzzer_dict_hex`.
- New standard/heavy campaigns opt into `havoc_deep_v210` (implies v28). The coverage path plants a few deterministic havoc mutants of the pack head into the libFuzzer corpus. Lite does not force v210. In-flight campaigns that already set the flag are left alone.
- libFuzzer argv adds `-mutate_depth` (8/12/16 by wall), `-entropic=1`, and `-use_value_profile=1`. Corpus merge after the wall is 30s, or 45s when the wall is at least 180s, and still stops when the parent deadline is gone.
- ASAN crash path: `abort_on_error=1:halt_on_error=1:symbolize=0:detect_leaks=0:allocator_may_return_null=1` on the libFuzzer session, the AFL shard, and oneshot inventory replay. Showmap stays non-abort so the map can be written, with `symbolize=0` and the existing 8s wall.
- Native crash replay allows inputs up to 65536 bytes (libFuzzer `-max_len`). The WASM 4096 hard ceil is unchanged and still applies to Dig.
- Worker log line includes `seeds_in`, `corpus_before`, `corpus_after`, and `subprocess`. `corpus=` is still the new-file delta, not the corpus size. Delta upload cap is 64. Edge prove default is 32 (`HACKME_HUNT_ENGINE_PROVE_MAX`, max 64).
- Crash-only `corpus_delta` does not resolve a coverage harness. `ProveHuntCorpusSeedEdges` returns before `ResolveCoverageHarness` when no seed has bytes. That call was compiling `afl-cc` for catalog targets (jsmn) on every crash upload, including unit tests that only assert crash-bucket dedupe. Corpus-delta tests also stub `resolveCoverageHarness` and `huntEngineSeedExec` so a seed upload cannot fall into `BuildAFLInstrumentedHarness` / `ReplayShard`. Production still builds the harness when there is at least one seed to prove.
- Worker and coordinator minimum wall floor is a full `MinEngineLeaseSec` (15s), matching `checkHuntEngineWallFloor`.

## Security

- Seed writes still go through `writeFileBeneath` (no symlink follow under the corpus root).
- Claim trim stays first-fit and is shared by claim and replay, so raising the item cap does not split the two sides.
- Inventory soft-skip releases the lease. It does not submit CLEAN and does not open a harness.
- The prove stub is test-only. Production `resolveCoverageHarness` is still `hunt.ResolveCoverageHarness`. Skipping the build when `provable == 0` does not skip crash ASAN replay (`researchCrashReplayer` runs later and is unchanged).
- Showmap still uses `abort_on_error=0` on purpose. Symbolize is off so prove cannot sit in the symbolizer; the 8s isolate wall remains the stall cap.
- Replay of a longer crash input is still coordinator-side, lease-scoped, and fail-closed when hunt replay is disabled. It does not raise the WASM check input ceiling.
- No new outbound fetch, no change to harness content-sha binding, no change to who may claim Hunt.

## Correctness

- Dig `PoolExecPerUnit` and byte-Hunt lease cap tests were not retargeted except the coverage-engine fleet default (90s wall → 210s lease) and a new 210s → 420s lease case.
- `iterations_per_shard` stored on an old campaign is not rewritten. New campaigns pick up the package default only when the key is absent. Coverage exec count stays 1.
- Havoc mutants are appended after the pack head, so the wrap seed remains the first corpus file.
- Subprocess stdin harnesses are labeled in the log. Pay is unchanged: a crash still full-pays, CLEAN without a proven edge stays on the floor. A subprocess lease is not given a higher pay grade.

## Tests

- `go test -count=1 -timeout 180s ./internal/hunt/ -skip 'BuildInventoryRust'` → ok (66s).
- `go test -count=1 -timeout 180s ./internal/workerfuzzloop/` → ok (0.4s).
- `go test -count=1 -timeout 120s -run 'TestLeaseSeconds|TestTrimSeedByteCorpus|TestApplySeedByteCorpus|TestClaimShipsSeedByteCorpus|TestHuntL2LibFuzzerSeedBootstrap|TestPoolExecPerUnit' ./internal/poolfuzz/` → ok (2s).
- `go test -count=1 -timeout 120s -run 'TestAcceptResearchCorpusDelta|TestLeaseSeconds|TestTrimSeedByteCorpus|TestApplySeedByteCorpus|TestClaimShipsSeedByteCorpus|TestHuntL2LibFuzzerSeedBootstrap' ./internal/poolfuzz/` → ok (6.5s). This includes `TestAcceptResearchCorpusDeltaStaleCrashNoPayCredit`.
- An earlier full `./internal/poolfuzz/` at 180s died in that stale-crash test inside `ProveHuntCorpusSeedEdges` → `ResolveCoverageHarness` → `BuildAFLInstrumentedHarness` → `LoadManifest`. Root cause: prove always compiled an AFL harness before looking at the seed list, so a crash-only upload blocked on `afl-cc`. Empty-seed return plus the test stub is the fix. A full package run that also compiles rust inventory harnesses is still outside this check (`TestBuildInventoryRustHarnessFuzzTarget` cargo hang).

## Follow-ups

- Full `./internal/poolfuzz` and `TestBuildInventoryRustHarnessFuzzTarget` were not re-run as a package. The rust test still needs `-skip` or its own timeout in CI. Corpus-delta tests no longer enter that AFL compile.
- Campaign L2 is still not pushed down onto the next worker as a shared in-process corpus. Upload of 64 new files is the compounding path.
- `MaxInputBytesHardCeil` stays 4096 for WASM/Dig. Only native Hunt crash replay uses 65536.
- Subprocess stdin leases are logged, not excluded from the floor. In-process `LLVMFuzzerTestOneInput` is still preferred when the pin has that entry.
- Old campaigns that already stored `engine_lease_sec` or `iterations_per_shard` keep those numbers until recreated.

---

## Independent audit — 2026-11-22

**Branch:** `feat/hunt-pool-power-deepen` (commits including deepen P0s, crash-only prove skip) 
**Auditor:** Cursor agent (read-only code review; no PR, no push) 
**Scope:** Seed corpus → libFuzzer/AFL, wall-clock & shard depth vs local LF, ASAN, dict/havoc, `ProveHuntCorpusSeedEdges` early return, claim seed shipping, tests/CI gaps.

### Executive summary

The deepen work is **directionally sound** for rc20 Hunt pool power: real engine walls (180/180/210s), reclaim lease headroom (480s cap), seed reservation ahead of guided snapshots, profile `-dict=`, LF tuning (`-mutate_depth` / entropic / value profile), bounded post-wall merge (30s / 45s), and coordinator ASAN crash replay at 65536 bytes. The **crash-only `ProveHuntCorpusSeedEdges` early return is correct in production** — it keys off empty `InputBytes` after `AcceptResearchCorpusDelta` filtering, not on “crashes present”; AFL/showmap is still built when any provable seed exists. Remaining risk is **test/CI coverage** and **structural pool-vs-local depth**, not a prod skip of real seed prove.

### Area notes

#### 1. Seed corpus → libFuzzer

- **Claim shipping:** `hunt_shard.go` trims then sets `SeedByteCorpus`; tests `TestClaimShipsSeedByteCorpus*` cover direct and L2-merge paths.
- **Worker install:** `collectCoverageEngineSeeds` reserves `seed_byte_corpus` first (96 cap); `InstallEngineSeedFiles` writes `claim-seed-<sha>.bin`; `SkipGenericBoot` avoids `{}` masking missing seeds.
- **Wire:** `TrimSeedByteCorpusForWire` 96 items / 256KiB aligns with worker 96-file cap; comment correctly notes rank cap 64 is for L2 cfg, not claim wire.
- **Gap:** No end-to-end test that a claim with 96 wire seeds produces `countCorpusFiles > 0` before `RunLibFuzzerSession` on a worker (unit tests cover collection only).

#### 2. Wall-clock / shard power vs single-PC LF

| SKU | Pool `engine_lease_sec` | `iterations_per_shard` (byte fallback) | Local reference |
|-----|---------------------------|----------------------------------------|-----------------|
| Lite | 180 | 96 | 1h / 20k iters |
| Standard | 180 | 288 | 8h / 200k iters |
| Heavy | 210 | 384 | 12h / 500k iters |

Coverage shards use `exec_per_unit=1` and wall = `engine_lease_sec`; reclaim `leaseSecondsForConfig` uses `max(2×wall, wall+120)` capped at **480s** (420s for 210s heavy). LF argv adds depth 8/12/16 by wall. **Pool remains time-boxed** vs overnight local; compounding is delta-upload (64), not persistent cross-miner LF corpus in one process (documented follow-up).

#### 3. ASAN options

- **LF/AFL shard:** `LibFuzzerASANOptions` = `detect_leaks=0:abort_on_error=1:halt_on_error=1:allocator_may_return_null=1:symbolize=0` — consistent on engine runner and coordinator crash replay.
- **Showmap prove:** `abort_on_error=0`, `symbolize=0`, 8s wall — intentional.
- **Merge:** `corpus_merge.go` uses non-abort ASAN for `-merge=1`; session post-wall merge budget 30s (45s if wall ≥ 180s) respects parent `ctx` deadline.

#### 4. Dict / havoc mutations

- **LF dict:** `EnsureLibFuzzerDict` + worker `coverageEngineDict` (claim hex → profile fallback).
- **Byte havoc on LF corpus:** `appendHavocSeedBurst` for `HavocDeepV28/V210` on claim; standard/heavy opt in via `ApplyHuntPowerScheduling` / `EnableDeepHavocV210`; lite does not force v210.
- **Concern:** Havoc burst capped at **4096** bytes vs LF `-max_len=65536`.

#### 5. `ProveHuntCorpusSeedEdges` early return (crash-only)

```go
// provable counts seeds with non-empty InputBytes (max cap scan)
if provable == 0 {
 return 0, nil // skips resolveCoverageHarness
}
```

- **Prod:** Crash-only uploads have no non-crash seeds with bytes → skip is correct; crashes validated via `researchCrashReplayer` (unchanged).
- **Not a prod bug:** Seed uploads with bytes still enter prove path and compile showmap harness.
- **Test fix (c47b10c5):** `researchDeltaHuntCamp` stubs `resolveCoverageHarness` + `huntEngineSeedExec` — fixes stale-crash test hang.
- **Missing:** Explicit `TestProveHuntCorpusSeedEdgesSkipsHarnessOnCrashOnly` / `...StillResolvesWhenSeedsPresent`.

#### 6. Pool claim shipping seeds

Verified: `ClaimedWork.SeedByteCorpus`, `LibFuzzerDict`, `EngineLeaseSec`, havoc flags on claim; coordinator emits `libfuzzer_dict_hex`.

#### 7. Regression / test gaps

| Gap | Risk |
|-----|------|
| `TestProveHuntCorpusSeedEdgesRequiresHarnessExec` without resolve stub | AFL compile in unit test |
| No crash-only vs seed prove regression | Reintroduce pre-prove AFL compile |
| Full `poolfuzz` + Rust inventory harness | CI blind spot |
| No claim test for stamped `libfuzzer_dict` on wire | Silent dict drop if coordinator encoding regresses |

### Recommendations (priority)

1. Add `TestProveHuntCorpusSeedEdgesCrashOnlySkipsResolve` and stub harness in existing prove test.
2. Add `TestClaimShipsLibFuzzerDictHex` (catalog jsmn).
3. CI: gate `poolfuzz` with `-skip BuildInventoryRust` or dedicated timeout for rust harness test.
4. Consider havoc burst `maxLen` aligned with LF `-max_len` for heavy/standard (product).
5. Document in ops runbook: recreate campaigns to pick up `engine_lease_sec` 180/210.

### Verdict

**Approve with test follow-ups.** Deepen P0s are implemented coherently; security posture unchanged and improved on crash-only upload latency. Blockers are not prod correctness of prove skip, but **regression harness** and **depth economics vs local LF**.

---

## Independent audit — power+network

**Date:** 2026-11-22. Uncommitted diff on `feat/hunt-pool-power-deepen`, plus `c5ead3bc`, `c47b10c5`, `d9fdb8e2`. No PR, no push.

**Verdict:** One seed-identity bug. Fixed. No crash-drop and no auth-retry defect in the power or network changes.

### Findings

1. **Medium (fixed).** `LoadLibFuzzerSeedFiles` deduped on the first 32 bytes. A newer unit that shared that prefix with an older file was discarded before the newest-first cap, so the sort never saw it. Dedup is now SHA-256 of the full input. `TestLoadLibFuzzerSeedFilesKeepsDistinctLongPrefix` passes.

Checked, not findings: `-len_control=0`, `-reduce_inputs=0`, `-ignore_crashes=1`, and `-detect_leaks=0` apply only on walls ≤600s; `-fork` is 1 below 180s, 2 from 180s, 3 from 300s in-process, and stdin stays capped at 2 even when `HACKME_HUNT_LF_FORK` is higher. Child libFuzzer still writes `artifact_prefix` before the fork parent honors `-ignore_crashes`. `PublishedHarnessIsLibFuzzer` keeps `HACKME_LF_STDIN_BIN` on the stdin driver. Execs are log-only. Accepted units are pinned to the front of `seed_byte_corpus` and boot starters go to the tail. Corpus upload and snapshot retry once on transient HTTP; 401/403 are not transient. Crash-only `ProveHuntCorpusSeedEdges` still returns before harness resolve. `MaxInputBytesHardCeil` and the WASM check ceiling stay 4096.

---

## In-process fleet vs one local libFuzzer — 2026-11-22

Uncommitted on `feat/hunt-pool-power-deepen` (HEAD `00aad9af`). No PR, no push, no commit.

### What changed

- A pool lease still materializes the published harness (content-sha bind stays fail-closed). If that ELF exports `LLVMFuzzerRunDriver`, it runs in-process. If the repo already has `LLVMFuzzerTestOneInput` for the catalog id (`cjson`, `libucl`, `mpack`, `cwalk`, `tinycbor`, `nghttp2`), the lease builds that dedicated binary and does not fork/exec the stdin ELF. Build failure falls back to the published stdin driver. `jsmn` has no dedicated source and stays on the stdin driver. `-fork` for stdin stays capped at 2.
- Miner A's accepted units are already written to the front of `seed_byte_corpus`. The next claim's bytes parse to those units, and the worker puts them first in the engine seed list even when the fat corpus snapshot was omitted (`CorpusLight`). No second fetch is required for that unit.
- `ExportLibFuzzerSeeds` no longer uses `os.Stat` / `os.WriteFile`. A planted file symlink is replaced in place; a directory symlink where the cache should be is refused. Neither writes the unit outside the cache.
- `InProcessShardsToMatchLocal` is the job-second break-even from the argv, not a measured overnight run. Assumption: equal exec/s per in-process job, and the next wall already holds the previous miner's units. Heavy 300s × `-fork=3` vs one 43200s process = **48** concurrent shards. Standard 240s × 2 vs 28800s = **60**. Lite 180s × 2 vs 3600s = **10**. One shard is 1/N of that budget.

### Comparison (this machine)

Same flags: `-max_total_time=3 -timeout=5 -detect_leaks=0 -print_final_stats=1 -rss_limit_mb=2048`, no `-fork`. `ASAN_OPTIONS=detect_leaks=0:abort_on_error=1:halt_on_error=1:allocator_may_return_null=1:symbolize=0`.

| Loop | Binary | Executed units | Final pulse | Wall clock |
|---|---|---|---|---|
| In-process | cached `mpack-libfuzzer-asan` | 40196 | 10049 exec/s | 4.32s |
| Stdin driver | `_subprocess-libfuzzer-asan` + a trivial ASAN reader | 85 | 21 exec/s | 4.17s |

The stdin number is the driver ceiling (the child only reads and exits). A catalog parser behind it is not faster. `-fork=2` doubles that storm; it does not turn it into the in-process rate. The 48/60/10 shard counts are not a 12-hour measurement and do not apply to jsmn.

**When the pool is faster:** several miners, each running an in-process harness, at or above that shard count, and each claim loads the units the previous miner just had accepted. Forty-eight concurrent heavy in-process shards cover the job-seconds of one 12-hour local process in a 300s wall.

**When it is not:** one 180–300s shard against a local process that already holds the corpus; any stdin-only target (jsmn); a fleet that does not load the previous wall's units.

### Audit of this diff

Pay rules, WASM 4096, crash-only prove (no AFL compile), and the single transient HTTP retry (not 401/403) are unchanged. No new outbound host: the catalog in-process build uses the existing `BuildLibFuzzerImport` clone URL, and a clone/compile error falls back to the published stdin binary. Crash artifacts are still collected from the session crash dir. Seed eviction via a 32-byte prefix stays fixed; the symlink follow in the L2 export was the write-side hole in this pass.

### Residual

- Stdin-only catalog targets stay at tens of exec/s. There is no in-process source to select.
- The first in-process catalog shard may spend lease time cloning and compiling. Later shards use the cache. `-fork` scaling (2 or 3 jobs) was not measured; the break-even assumes each job matches one local job.
- A miner that drops `seed_byte_corpus` still starts from whatever is already on its disk.

### Tests

- `go test -count=1 -timeout 180s ./internal/hunt/ -skip 'BuildInventoryRust'` → ok (71.0s). An earlier parallel run with `./internal/poolfuzz` failed `TestReplayShardDetectsIntentionalCrash` on the 8s isolate wall; alone it passed (16.8s).
- `go test -count=1 -timeout 180s ./internal/workerfuzzloop/` → ok (2.9s).
- `go test -count=1 -timeout 180s ./internal/poolfuzz/ -skip 'BuildInventoryRust'` → killed at 180.1s while a later test was inside sqlite migrate (`TestAcceptResearchCorpusDeltaNextClaimKeepsSeed` had run 0s). Not an AFL compile. Not skipped.
- Same package with `-timeout 300s` → ok (180.9s).
- `go test -count=1 -timeout 90s ./internal/poolsync/ -run 'TestUploadCorpusNamespace|TestSettleOutbox'` → ok (1.2s).
- `TestBuildInventoryRustHarnessFuzzTarget` was not run.

---

## Coordinator stays light, catalog binary stays cached — 2026-11-22

Uncommitted work on `00aad9af` kept. No pull request.

### Coordinator stays light because

- A Hunt claim does not walk the L2 seed directory once `seed_byte_corpus` already holds a coverage unit. Delta accept pinned that unit at the front. Empty or boot-only campaigns still merge once. Replay uses the same check, so claim and replay stay on the same bytes. No compiler runs in `EnsureGuidedCorpusSeeded`.
- `corpus_delta` still stores seeds, then prove. Crash-only prove returns before `ResolveCoverageHarness`. When seeds exist, `BuildTargetAFL` / `BuildTarget` stat the cached binary before `git fetch` or clang/afl-cc. The showmap/replay loop stops after 12s (`HACKME_HUNT_ENGINE_PROVE_BUDGET_MS`, 1s–30s). Seeds past that stay stored. There is no extra queue.
- Fresh units copied into campaign JSON are capped at the existing wire budget (96 items / 256KiB) before the hex copy. The merge stops once that head is full.
- A CLEAN coverage submit returns before ASAN replay. It does not compile a harness or run showmap.

### Power change

- An in-process catalog binary (`cjson`, `libucl`, `mpack`, `cwalk`, `tinycbor`, `nghttp2`) is reused from `.cache/hunt-lf-import/<id>-libfuzzer-asan`. A hit does not call clang and does not load the manifest. A miss clones sources only (`EnsureClone`); it does not also compile the unused stdin ASAN driver. Shards share one build lock so two leases do not clang the same path. `jsmn` stays on the stdin driver.
- The worker still writes `seed_byte_corpus` first. The cached binary does not reorder those files.

### Bug pass

- `catalogInProcessBinUsable` required a non-stub read. A symlink failed that read and was then treated as the in-process target. It now requires a regular file. Cache writes (`writeCachedBin`, `writeLibFuzzerCacheBin`) unlink a planted symlink instead of following it.
- Auth retry is unchanged: one transient HTTP retry, and 401/403 are not transient.
- Pay functions are unchanged. A proven edge still full-pays; CLEAN without one stays on the floor.
- The pinned unit stays at the front of `seed_byte_corpus`. A later L2 file is not merged ahead of it on the next claim.

### Tests

`ASAN_OPTIONS` unset. `TestBuildInventoryRustHarnessFuzzTarget` not run.

- `go test -count=1 -timeout 180s ./internal/hunt/ -skip 'BuildInventoryRust'` → ok (77.3s).
- `go test -count=1 -timeout 180s ./internal/workerfuzzloop/` → ok (2.4s).
- `go test -count=1 -timeout 300s ./internal/poolfuzz/ -skip 'BuildInventoryRust'` → ok (187.5s).
- `go test -count=1 -timeout 90s ./internal/poolsync/ -run 'TestUploadCorpusNamespace|TestSettleOutbox'` → ok (1.1s).
