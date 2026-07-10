# OSS CVE Hunt — real upstream ASAN fuzz

## OSS CVE Watch (14-day spotlight)

Daily single-repo hunt with public ledger pages:

- Hub: https://hackme.tech/reports/oss-cve-watch/
- Run: `bash scripts/ops/run_oss_cve_watch_day.sh` (see `TARGET=nghttp2` in script)
- Export: `scripts/ops/export_oss_cve_watch_html.py`

Use this for **social cadence** (one library, one post/day). The wave hunt below is the **batch matrix** across many parsers.

### libFuzzer depth (nghttp2 focus · Days 2–14)

Coverage-guided local hunt — corpus persists between sessions. Day 1 Watch stays mutation; Day 2+ uses libFuzzer.

```bash
# Build once
TARGET=nghttp2 bash scripts/ops/build_oss_libfuzzer.sh

# Night session (8h default, corpus grows)
bash scripts/ops/run_oss_libfuzzer_session.sh
MAX_TIME=3600 bash scripts/ops/run_oss_libfuzzer_session.sh   # 1h test

# Background
setsid bash scripts/ops/run_oss_libfuzzer_session.sh >>logs/nghttp2-libfuzzer.nohup.log 2>&1 &

# OSS CVE Watch Day 2+ publish (optional)
DAY=2 MAX_TIME=7200 bash scripts/ops/run_oss_cve_watch_libfuzzer_day.sh
DAY=2 SKIP_PUBLISH=1 bash scripts/ops/run_oss_cve_watch_libfuzzer_day.sh
```

Corpus: `reports/oss-cve-libfuzzer/nghttp2/corpus/` · Sessions: `.../sessions/` · Latest: `LATEST_SESSION`.

---

| Rule | Why |
|------|-----|
| **One public watch day at a time** | Hub shows Day N only after you export + deploy |
| **Background runs use `SKIP_PUBLISH=1`** | `bash scripts/ops/run_oss_cve_watch_background.sh` keeps reports in `reports/` only |
| **Per day: go deeper OR switch repo** | Day 1–3 nghttp2 depth ramp *or* Day 2 = md4c — pick one story per publish |
| **Manual publish** | `python3 scripts/ops/export_oss_cve_watch_html.py DAY REPORT_DIR` then `deploy_hackme_site.sh` |

Do **not** chain `export_oss_cve_watch_html.py` for multiple days in one night — that defeats the 14-day ledger narrative.

**Deep matrix hunts (day+night, no watch publish):**

```bash
setsid bash scripts/ops/run_oss_cve_deep_daynight.sh >>logs/oss-cve-deep-daynight.nohup.log 2>&1 &
tail -f logs/oss-cve-deep-daynight.nohup.log
```

Queue: duktape → compression → regex → libxml2 → md4c → ranked wave — reports land in `reports/oss-cve/deep-*` only.

---

Tier **oss_cve** runs mutation fuzz on **cloned upstream repos** with stdin ASAN drivers — not WASM excerpt guards.

## Targets

See `upstream/oss_cve_targets.json` (12 targets):

| Priority | Targets |
|----------|---------|
| 1 | md4c, cJSON, centijson |
| 2 | jsmn, mjson, yyjson, parson, jansson, tomlc99, expat |
| 3 | inih, sheredom |

## Run

```bash
# Full hunt (priority 1 parsers, ~60k iter each)
bash scripts/ops/run_oss_cve_hunt.sh

# Fast subset
TARGETS=md4c,cjson BUDGET=10000 TIME_LIMIT=120 bash scripts/ops/run_oss_cve_hunt.sh

# CI gate
bash scripts/ops/oss_cve_gate.sh

# Nightly rotation (2 targets/day, skips centijson until disclosed)
bash scripts/ops/run_oss_cve_nightly.sh

# Priority-2 sweep (excludes centijson)
TARGETS=jsmn,mjson,yyjson,parson,jansson,tomlc99,expat BUDGET=50000 TIME_LIMIT=300 bash scripts/ops/run_oss_cve_hunt.sh
```

## Verdicts

| Verdict | Meaning |
|---------|---------|
| `CLEAN` | No ASAN crash in budget — publish methodology post |
| `CVE_CANDIDATE` | Crash on real upstream — **HOLD**, responsible disclosure |

## Disclosure workflow

1. Minimize crash input in `reports/oss-cve/*/crashes/`
2. Repro with built binary + `echo -ne ... | ./fuzz.bin`
3. Email maintainer (security@ or GitHub advisory)
4. Publish case study only after fix or timeout

# After fix: set status → `fixed`, then `published`, fill `cve_id`, `fix_url`, `show_repro: true` in `upstream/oss_cve_cases.json`, run:

```bash
python3 scripts/ops/export_oss_cve_cases.py
```

Public hub: `web/site/reports/oss-cve/` — case cards without repro until published.

## Depth tier

Campaign config: `"depth_tier": "oss_cve"` — triggers post-WASM upstream hunt on matching `wasm_guard`.
