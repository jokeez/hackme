# OSS CVE Watch · libheif series (Days 1–14)

**Started:** 2026-07-20  
**Status (2026-08-07):** Days **1–14 CLEAN** · **series CLOSED**  
**Target:** [strukturag/libheif](https://github.com/strukturag/libheif) · upstream `file_fuzzer`  
**Public ledger:** https://hackme.tech/reports/oss-cve-watch-libheif/  
**Finale:** https://hackme.tech/reports/oss-cve-watch-libheif/day14.html  
**Prior series:** nghttp2 14/14 CLEAN — [verdict](verdicts/OSS_CVE_WATCH_NGHTTP2_SERIES_VERDICT.md)

## Naming

This is a **separate series** from nghttp2 (different harness, corpus, and ledger).

## Progress

| Day | Verdict | Notes |
|-----|---------|--------|
| 1–14 | CLEAN | All ledgers `day01.html`…`day14.html` on hub + GitHub `main` |

**Series totals (frozen ledger):** ~2.57B libFuzzer exec · ~325h ASAN depth · ASAN crashes **0** · corpus **3215** · edges **7509** (Day 14).

CLEAN = no ASAN heap crash in that day’s budget. **Not** “proven secure.” **Not** a CVE claim.

## After Day 14

No automatic Day 15. Next research is optional short pilots or B2B-shaped campaigns — not another 14-day marathon by default.

## Reproduce / ops

Corpus persists under `reports/oss-cve-libfuzzer/libheif/corpus/`.

```bash
# One-off session (operator)
bash scripts/ops/run_oss_libfuzzer_session.sh
```

Historical cadence helpers remain under `scripts/ops/run_oss_cve_watch_libheif_*` for archive/repro only.
