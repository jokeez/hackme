# OSS CVE Hunt — real upstream ASAN fuzz

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
