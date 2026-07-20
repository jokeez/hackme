# OSS CVE Watch · libheif series (Days 1–14)

**Started:** 2026-07-20  
**Target:** [strukturag/libheif](https://github.com/strukturag/libheif) · upstream `file_fuzzer`  
**Prior series:** nghttp2 14/14 CLEAN — [verdict](verdicts/OSS_CVE_WATCH_NGHTTP2_SERIES_VERDICT.md)

## Naming

This is **Day 1/14 of a new series**, not “Day 15” of nghttp2. Different harness, corpus, and ledger.

## Run

```bash
# Build (needs clang++, cmake, libde265-dev, libdav1d-dev)
TARGET=libheif bash scripts/ops/build_oss_libfuzzer_libheif.sh

# Session (corpus persists)
TARGET=libheif MAX_TIME=28800 bash scripts/ops/run_oss_libfuzzer_session.sh

# VPS (operator hub — libheif built under .cache/oss-cve-clones/libheif)
TARGET=libheif MAX_TIME=28800 setsid bash scripts/ops/run_oss_libfuzzer_session.sh \
  >>logs/libheif-libfuzzer.nohup.log 2>&1 &
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
