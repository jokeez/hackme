# OSS CVE Watch — nghttp2 series verdict (Days 1–14)

**Date:** 2026-07-20  
**Target:** [nghttp2](https://github.com/nghttp2/nghttp2) · HTTP/2 session `mem_recv`  
**Engine:** Day 1 stdin mutation · Days 2–14 libFuzzer + ASAN  
**Public hub:** https://hackme.tech/reports/oss-cve-watch/

## Verdict

| Field | Result |
|-------|--------|
| **Series outcome** | **CLEAN** — 0 ASAN heap crashes across all published days |
| **Days completed** | 14/14 |
| **CVE candidates** | 0 submitted |
| **Coverage edges (final)** | 467 (plateau from ~Day 2–3) |
| **Corpus (final)** | 420 inputs · 305KB |

## Cumulative metrics (Days 2–14, libFuzzer)

| Metric | Value |
|--------|-------|
| Executions | **14,321,831,235** (~14.32B) |
| Operator wall time | **~114.5h** |
| ASAN heap crashes | **0** |
| Day 14 alone | 3,029,631,004 exec · 17.0h · ~49,622 exec/s |

Day 1 (mutation baseline): 44,033 iterations · UBSan signals · not included in libFuzzer cumulative.

## Interpretation (honest)

1. **CLEAN is a session-budget statement**, not a security certificate. We found no ASAN-reported heap corruption in our harness and time budget on an upstream clone.
2. **Coverage plateau at 467 edges** means later days added execution depth, not new edge discovery — expected for a narrow mem_recv harness on a mature library.
3. **No CVE id** without coordinated disclosure, minimal PoC, and maintainer triage — none triggered.
4. **Diminishing returns** on the same nghttp2 surface: further nights would mostly repeat the plateau unless the harness expands (new API paths, stateful sessions, interleaved frames).

## Gate & publish integrity

Publish script `scripts/ops/publish_oss_cve_watch_day_finish.sh` refuses stub sessions:

- MIN_ITERATIONS ≥ 50M  
- MIN_ELAPSED_SEC ≥ 3600  
- corpus_count > 0 · coverage_edges > 0  

All 14 days passed gate before HTML + news feed update.

## Recommended next target

**libheif — Day 1/14 of a new series** (not “Day 15” of nghttp2). New target, harness, corpus, and ledger.

See `reports/oss-cve-watch/DAY15_TARGET_RECOMMENDATION.md` (operator notes; filename legacy).

| Priority | Target | Rationale |
|----------|--------|-----------|
| **Primary** | **libheif** | 2026 High heap R/W CVE cluster · HEIF/AVIF · fresh corpus |
| **Backup** | **Exiv2** | Write-path metadata · “bugs survive OSS-Fuzz” narrative |

**Bug bounty note:** nghttp2 has no meaningful cash program for this campaign style. libheif/Exiv2 are **research + GHSA** paths first; cash bounty only if a finding maps to an eligible consumer program. Pure H1-style bounty hunting is a poor fit for ASAN C++ decode fuzz (curl H1 closed; Cloudflare quiche = protocol DoS, different stack).

Do **not** mix nghttp2 corpus with libheif. Use `TARGET=libheif` · `scripts/ops/build_oss_libfuzzer_libheif.sh` · corpus under `reports/oss-cve-libfuzzer/libheif/`.

## Operator checklist (post-series)

- [x] Day 14 HTML live · meta.json · news-feed updated  
- [x] Series verdict doc (this file)  
- [ ] Social: X thread · Discord · Bitcointalk ANN · Telegram (copy in `docs/social/OSS_CVE_WATCH_DAY14_SOCIAL.md`)  
- [ ] Day 15 harness bootstrap (libheif build + seeds)  
- [ ] HMS / settlement economics unchanged by research pivot  
