# Bitcoin Core 30-day fuzz series

Live daily fuzz of upstream Bitcoin Core WASM guards on local HackMe node. One module per day; artifacts under `reports/bitcoin30/`.

## Run

```bash
DAY=1 bash scripts/ops/run_bitcoin30_day.sh
```

Current report: `reports/bitcoin30/CURRENT/`

## Schedule (days 1–5)

| Day | Module | WASM | Core reference |
|-----|--------|------|----------------|
| 1 | GetScriptOp · 520 B cap | `upstream_bitcoin_getscriptop.wasm` | `script.cpp` GetScriptOp · `script.h` MAX_SCRIPT_ELEMENT_SIZE |
| 2 | HasValidOps | `upstream_bitcoin_hasvalidops.wasm` | `script.cpp` HasValidOps |
| 3 | CheckTransaction · MoneyRange | `upstream_bitcoin_tx_check.wasm` | `tx_check.cpp` · `amount.h` |
| 4–30 | TBD | extend `run_bitcoin30_day.sh` | see `docs/BITCOIN_CORE_OFFICIAL_LINKS.md` |

## Social copy (English)

| Day | Telegram | X | Bitcointalk |
|-----|----------|---|-------------|
| 1 | `docs/social/BITCOIN30_DAY01_TELEGRAM.txt` | `docs/social/BITCOIN30_DAY01_X.txt` | `docs/social/BITCOIN30_DAY01_BITCOINTALK.txt` |
| 2 | `docs/social/BITCOIN30_DAY02_TELEGRAM.txt` | `docs/social/BITCOIN30_DAY02_X.txt` | `docs/social/BITCOIN30_DAY02_BITCOINTALK.txt` |

Pilot (all channels): `docs/social/FUZZ_PILOT5_*.txt`

## Disclaimer

WASM `check(i64)→i32` guards inspired by Core — not a full node fork. Guard signals require native upstream validation before any CVE claim.
