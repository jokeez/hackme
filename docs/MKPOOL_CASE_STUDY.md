# mkpool parser fuzz — case study

Voluntary stratum/SV2 parser-boundary research for [Mecanik/mkpool](https://github.com/Mecanik/mkpool) (GitHub issue #2).

## Verdict

| Metric | Value |
|--------|-------|
| Total runs | 16,000 (4 guards × 4,096 budget) |
| Critical | **0** |
| Guard signals | 400 (expected reject/truncate paths) |
| Patch requested | **No** |

## Guards (WASM)

| Guard | Source inspiration |
|-------|-------------------|
| `sv2_reader_bounds` | `sv2_codec.hpp` Reader OOR / B0_32 |
| `version_mask` | `stratum_protocol.cpp` `validate_version` |
| `submit_hex_fields` | `client_session.cpp` `mining.submit` hex rules |
| `v1_line_frame` | V1 buffer cap (1 MiB) |

Sources: `tasks/sources/security/mkpool/*.c`

## Run

```bash
bash scripts/build_mkpool_fuzz_pack.sh
bash scripts/ops/run_mkpool_stratum_fuzz_pack.sh
```

Export public HTML (fuzz_report_v2, same as node `#fuzz`):

```bash
python3 scripts/ops/export_mkpool_fuzz_report_html.py \
  reports/mkpool-fuzz/mkpool-fuzz-<stamp> \
  web/site/reports/mkpool-fuzz
```

## Public report

https://hackme.tech/reports/mkpool-fuzz/

## Honest scope

Synthetic property guards mapped from mkpool sources — **not** native ASAN on their binary. Guard signals ≠ CVE. See research hub: https://hackme.tech/research.html
