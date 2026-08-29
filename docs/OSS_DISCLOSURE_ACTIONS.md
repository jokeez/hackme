# OSS disclosure — status (2026-08-29)

| Project | Issue | Status |
|---------|-------|--------|
| centijson | [mity/centijson#13](https://github.com/mity/centijson/issues/13) | triage |
| libucl | [vstakhov/libucl#395](https://github.com/vstakhov/libucl/issues/395) (+ dup [#396](https://github.com/vstakhov/libucl/issues/396)) | triage |
| cfgpack | [Arsievert/cfgpack#1](https://github.com/Arsievert/cfgpack/issues/1) (+ dup [#2](https://github.com/Arsievert/cfgpack/issues/2)) | triage |

**Policy:** No hackme.tech publish until fix merged or maintainer confirms security + CVE path.

## If maintainer replies

| Response | Action |
|----------|--------|
| Fix merged | Update `oss_cve_cases.json` → `fixed`, add `fix_url` |
| Confirmed security | Ask them to file GitHub Security Advisory / CVE; you get credit |
| "Not a bug" / won't fix | Close internal case; no public post |
| 90 days silence | Polite bump on issue; then limited advisory (still no weaponized PoC) |

## Do not do yet

- Deploy repro to hackme.tech
- Telegram/X posts with hex payloads
- MITRE CVE request without maintainer ack (libucl maybe later via #396)
