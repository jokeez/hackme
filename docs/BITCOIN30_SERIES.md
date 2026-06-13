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
| 2 | HasValidOps | `upstream_bitcoin_hasvalidops.wasm` | `script.cpp` HasValidOps · live `day02-20260611T135419Z` (62 guard signals, 0 critical) |
| 3 | CheckTransaction · MoneyRange | `upstream_bitcoin_tx_check.wasm` | `tx_check.cpp` · `amount.h` · live `day03-20260612T134723Z` (verdict clean, 0 findings, 57 edges) |
| 4 | CheckTransaction · duplicate inputs | `upstream_bitcoin_tx_dup_inputs.wasm` | `tx_check.cpp` CVE-2018-17144 · live `day04-20260613T133200Z` (5 guard signals, 0 critical) |
| 5 | EvalScript · SCRIPT_ERR_PUSH_SIZE | `upstream_bitcoin_evalscript_push.wasm` | `interpreter.cpp` L457–458 · live `day05-20260613T201400Z` (59 guard signals, 0 critical) |
| 6–30 | TBD | extend `run_bitcoin30_day.sh` | see `docs/BITCOIN_CORE_OFFICIAL_LINKS.md` |

## Social copy (English, local only)

Drafts live under `docs/social/` (gitignored). After each `DAY=N` run, update Telegram / X thread / Bitcointalk from `reports/bitcoin30/CURRENT/DAY_SUMMARY.json`.

## Disclaimer

WASM `check(i64)→i32` guards inspired by Core — not a full node fork. Guard signals require native upstream validation before any CVE claim.
