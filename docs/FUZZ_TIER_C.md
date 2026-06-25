# Fuzz Tier C — upstream_binary + ASAN harness repro

## Fourth depth tier

| Tier | Key | Native repro | Bounty gate |
|------|-----|--------------|-------------|
| ① | `wasm_only` | off | WASM finding |
| ② | `wasm_native` | Go port | `native_confirmed` |
| ③ | `bytes_corpus` | Go port + byte inputs | `native_confirmed` |
| **④** | **`upstream_binary`** | **ASAN clang harness** | **`confirmed` or `native_crash`** |

## Security model

- Harness sources **allowlisted** under `tasks/sources/security/upstream/*.c` only (path traversal blocked).
- ASAN binaries built in temp dir, cached under `.cache/native-repro/` (content-hash keyed).
- Subprocess **5s timeout**, minimal env (`ASAN_OPTIONS` leak detection off for guard probes).
- **`native_crash`** ≠ automatic CVE — requires maintainer triage before disclosure.
- Bounty escrow unlocks on `confirmed` (guard) or `native_crash` (ASAN signal).

## Example

**OSS hunt · `dogecoin_hasvalidops` · `upstream_binary`**

```
WASM run     → 120 guard signals
ASAN repro   → 120 native_confirmed (same pinned C logic, real clang binary)
ASAN crash   → 0 (expected — guards are policy checks, not memory bugs)
Publish gate → ROTATE_CLEAN unless native_crash
```

**If ASAN crash appears (future OSS parser target):**

```
Status: native_crash
Bounty: eligible (held until triage)
Publish: blocked until responsible disclosure
```

## Usage

```bash
# Campaign config
{
  "depth_tier": "upstream_binary",
  "guard_name": "bitcoin_evalscript_push",
  "native_repro_mode": "asan_binary",
  "bounty_requires_native": true
}

# Bitcoin30 days 28–30 auto-select upstream_binary
DAY=28 bash scripts/ops/run_bitcoin30_day.sh

# OSS rotation (default tier)
bash scripts/ops/run_oss_pr_fuzz_hunt.sh

# CI gate
bash scripts/ops/fuzz_tier_c_gate.sh
```

## Honest scope

Tier C compiles **our pinned harness excerpts** with ASAN — not full `bitcoind` / `go-ethereum` binaries. Next: libFuzzer corpora from `upstream/pins.json` qa-assets for differential fuzz (v4.1).
