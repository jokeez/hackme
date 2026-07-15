# OSS CVE Hunt — Wave 2 Verdict (2026-06-26)

Report: `reports/oss-cve/20260626T021500Z/`

**Budget:** 80k iter/target · **Time:** 900s · **Targets:** 9 (excl. centijson HOLD)

## Rollup

| Verdict | Targets |
|---------|---------|
| **CVE_CANDIDATE** | **libucl**, **cfgpack** |
| **CLEAN** | cyaml, tomlc17, parsello, cj5, libconfini, inih, sheredom |

**Status: HOLD** — no public repro until maintainer triage.

---

## libucl (vstakhov/libucl)

- **Crash class:** UBSan — `ucl_hash.c:275` via `ucl_parser_free` / `ucl_object_unref`
- **Minimized input:** `{"a":1}{"a":1}` (two JSON objects concatenated, 14 bytes)
- **Variants:** 271 crash artifacts (likely one bug class — double-parse / refcount)
- **Repro:**
  ```bash
  echo -n '{"a":1}{"a":1}' | .cache/oss-cve-bin/libucl-*.bin
  ```
- **Disclosure:** GitHub issue to https://github.com/vstakhov/libucl

---

## cfgpack (Arsievert/cfgpack)

- **Crash class:** UBSan — signed integer left-shift overflow in `cfgpack_msgpack_decode_int64` (`msgpack.c:294`)
- **Variants:** 68 artifacts (same decoder path)
- **Disclosure:** GitHub issue to https://github.com/Arsievert/cfgpack

---

## CLEAN (methodology publish OK)

| Target | Iterations |
|--------|------------|
| cyaml | 14,455 |
| tomlc17 | 17,096 |
| parsello | 18,448 |
| cj5 | 19,752 |
| libconfini | 23,604 |
| inih | 21,496 |
| sheredom | 22,831 |

---

## Prior HOLD

- **centijson** — `{"":1}` UBSan `value.c:438` (maintainer contacted)
