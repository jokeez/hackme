# Bitcoin Core 30-day fuzz series

Live daily fuzz of upstream Bitcoin Core WASM guards on local HackMe node. One module per day; artifacts under `reports/bitcoin30/`.

## Run

```bash
DAY=8 bash scripts/ops/run_bitcoin30_day.sh
```

Public day-8 report: [hackme.tech/reports/bitcoin30-day08.html](https://hackme.tech/reports/bitcoin30-day08.html)

Current report: `reports/bitcoin30/CURRENT/`

Public week-1 ledger: [hackme.tech/reports/bitcoin30-week1.html](https://hackme.tech/reports/bitcoin30-week1.html)

## Schedule (days 1–8 · week 2 started)

| Day | Module | WASM | Live result |
|-----|--------|------|-------------|
| 1 | GetScriptOp · 520 B cap | `upstream_bitcoin_getscriptop.wasm` | 64 runs · 59 guard signals |
| 2 | HasValidOps | `upstream_bitcoin_hasvalidops.wasm` | 64 runs · 62 guard signals |
| 3 | CheckTransaction · MoneyRange | `upstream_bitcoin_tx_check.wasm` | **clean** · 0 findings |
| 4 | CheckTransaction · duplicate inputs | `upstream_bitcoin_tx_dup_inputs.wasm` | CVE-2018-17144 class · 5 signals |
| 5 | EvalScript · SCRIPT_ERR_PUSH_SIZE | `upstream_bitcoin_evalscript_push.wasm` | 59 guard signals |
| 6 | SegWit witness stack · push cap | `upstream_bitcoin_witness_stack.wasm` | 128 runs · 80 signals |
| 7 | EvalScript · SCRIPT_ERR_OP_COUNT | `upstream_bitcoin_evalscript_opcount.wasm` | 64 runs · 56 signals · `day07-20260616T110000Z` |
| 8 | EvalScript · SCRIPT_ERR_STACK_SIZE | `upstream_bitcoin_evalscript_stack.wasm` | 64 runs · 64 guard signals · `day08-20260617T120000Z` |
| 9–30 | TBD | extend `run_bitcoin30_day.sh` | block weight · coinbase · taproot |

### Week 1 verdict

576 total runs · 321 guard signals · **0 critical** · 1 clean module (Day 3). Guard signals = boundary-class triage, not CVE claims.

## Social copy (English, local only)

Drafts live under `docs/social/` (gitignored). After each `DAY=N` run, update Telegram / X thread / Bitcointalk from `reports/bitcoin30/CURRENT/DAY_SUMMARY.json`.

## Disclaimer

WASM `check(i64)→i32` guards inspired by Core — not a full node fork. Guard signals require native upstream validation before any CVE claim.
