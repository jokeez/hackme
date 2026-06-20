# Bitcoin Core 30-day fuzz series

Live daily fuzz of upstream Bitcoin Core WASM guards on local HackMe node. One module per day; artifacts under `reports/bitcoin30/`.

## Run

```bash
DAY=8 bash scripts/ops/run_bitcoin30_day.sh
```

Public day-8 report: [hackme.tech/reports/bitcoin30-day08.html](https://hackme.tech/reports/bitcoin30-day08.html)

Public day-11 report: [hackme.tech/reports/bitcoin30-day11.html](https://hackme.tech/reports/bitcoin30-day11.html)

Series hub: [hackme.tech/reports/bitcoin30.html](https://hackme.tech/reports/bitcoin30.html)

mkpool case study: [docs/MKPOOL_CASE_STUDY.md](MKPOOL_CASE_STUDY.md) · report: [hackme.tech/reports/mkpool-fuzz/](https://hackme.tech/reports/mkpool-fuzz/)

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
| 9 | Block weight · MAX_BLOCK_WEIGHT | `upstream_bitcoin_block_weight.wasm` | 128 runs · 80 guard signals · `day09-20260618T030511Z` |
| 10 | BIP34 coinbase height | `upstream_bitcoin_coinbase_bip34.wasm` | 128 runs · 80 guard signals · `day10-20260618T122526Z` |
| 11 | Duplicate inputs · deep | `upstream_bitcoin_tx_dup_inputs.wasm` | 256 runs · 24 guard signals · CVE-2018-17144 class · `day11-20260618T122959Z` |
| 12 | EvalScript stack · deep | `upstream_bitcoin_evalscript_stack.wasm` | 256 runs |
| 13 | Witness stack · deep | `upstream_bitcoin_witness_stack.wasm` | 256 runs |
| 14 | EvalScript op count · deep | `upstream_bitcoin_evalscript_opcount.wasm` | 256 runs |
| 15–30 | TBD | extend `run_bitcoin30_day.sh` | taproot · new guards |

### Week 2 schedule (5 days)

```bash
# One per day (09:00 UTC optional timer):
DAY=10 bash scripts/ops/run_bitcoin30_day.sh

# Or automated block:
bash scripts/ops/start_bitcoin30_5days.sh
# catch-up (no wait): WAIT_SEC=0 bash scripts/ops/start_bitcoin30_5days.sh
```

Social drafts: `docs/BITCOIN30_SOCIAL_WEEK2.md`

### Week 1 verdict

576 total runs · 321 guard signals · **0 critical** · 1 clean module (Day 3). Guard signals = boundary-class triage, not CVE claims.

## Social copy (English, local only)

Drafts live under `docs/social/` (gitignored). After each `DAY=N` run, update Telegram / X thread / Bitcointalk from `reports/bitcoin30/CURRENT/DAY_SUMMARY.json`.

## Disclaimer

WASM `check(i64)→i32` guards inspired by Core — not a full node fork. Guard signals require native upstream validation before any CVE claim.
