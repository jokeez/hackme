# OSS CVE Watch · libheif series (Days 1–14)

**Started:** 2026-07-20  
**Target:** [strukturag/libheif](https://github.com/strukturag/libheif) · upstream `file_fuzzer`  
**Prior series:** nghttp2 14/14 CLEAN — [verdict](verdicts/OSS_CVE_WATCH_NGHTTP2_SERIES_VERDICT.md)

## Naming

This is **Day 1/14 of a new series**, not “Day 15” of nghttp2. Different harness, corpus, and ledger.

## Run

### 24/7 · fixed 24h days (recommended)

Each day = **exactly 86400s** from series anchor (Day 1 start). Fuzzer chains automatically; publish at day boundary.

```bash
# VPS hub — install once, then start
bash scripts/ops/install_libheif_24h_cadence_systemd.sh
ANCHOR_EPOCH=1784543563 systemctl --user start hackme-libheif-24h.service

# Or foreground (adopts live fuzzer if running)
ANCHOR_EPOCH=1784543563 bash scripts/ops/run_oss_cve_watch_libheif_24h_cadence.sh
```

State: `reports/oss-cve-watch-libheif/cadence.json` · logs: `logs/oss-cve-watch-libheif-24h-*.log`

Publish gate (libheif): ≥20M exec · ≥23h wall per day.

### Manual one-off session

```bash
TARGET=libheif bash scripts/ops/build_oss_libfuzzer_libheif.sh
TARGET=libheif MAX_TIME=86400 bash scripts/ops/run_oss_libfuzzer_session.sh
```

## Paths

| Path | Role |
|------|------|
| `reports/oss-cve-libfuzzer/libheif/corpus/` | Persistent corpus |
| `reports/oss-cve-libfuzzer/libheif/crashes/` | ASAN artifacts |
| `tasks/seeds/oss-libfuzzer/libheif/` | Optional local seeds |
| `.cache/oss-cve-clones/libheif/fuzzing/data/corpus/` | Upstream OSS-Fuzz seeds (bootstrap) |

## Disclosure

Research-first → GitHub Security Advisory on libheif. Cash bounty only if finding maps to an eligible consumer program.

## Operator notes

Full target analysis: `reports/oss-cve-watch/DAY15_TARGET_RECOMMENDATION.md` (local; legacy filename).
