# Bitcoin Core — 5-module research (HackMe useful-PoW + property fuzz)

**Date:** 2026-05-25 · **Upstream:** [bitcoin/bitcoin](https://github.com/bitcoin/bitcoin) `master`  
**Honest scope:** We test **WASM guards derived from Core logic**, not the full C++ node. No claim of a new Bitcoin Core CVE unless coordinated with maintainers.

## Central Core map (where the real logic lives)

| # | Bitcoin Core (canonical) | Guard in HackMe repo |
|---|--------------------------|----------------------|
| 1 | [`src/script/script.h`](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.h) — `MAX_SCRIPT_ELEMENT_SIZE = 520` | `rust_script_push_bounds_guard` |
| 2 | [`src/script/script.cpp`](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.cpp) — `HasValidOps()` / `GetScriptOp()` push parse | `rust_bounds_guard` (safe window predicate) |
| 3 | [`src/consensus/tx_check.cpp`](https://github.com/bitcoin/bitcoin/blob/master/src/consensus/tx_check.cpp) — output/value sanity (`MoneyRange`, no negative) | `rust_overflow_guard` |
| 4 | [`src/validation.cpp`](https://github.com/bitcoin/bitcoin/blob/master/src/validation.cpp) — tx/block state acceptance pipeline | `rust_state_transition_guard` |
| 5 | [`src/script/interpreter.cpp`](https://github.com/bitcoin/bitcoin/blob/master/src/script/interpreter.cpp) — `EvalScript` / opcode dispatch | `cpp_script_push_bounds_guard` (C++ twin of #1) |

Module **5** is the same consensus class as **1** in a second language (shows cross-language WASM gates).

## Visual report (HTML)

Open in browser (screenshots for Bitcointalk / Telegram):

- **Local file:** `docs/reports/bitcoin-core-5module-report.html`
- **After site deploy:** https://hackme.tech/reports/bitcoin-core-5module.html

Regenerate:

```bash
bash scripts/build_security_task_pack.sh
go run ./tools/bc5_report
```

## How we test (full node)

```bash
bash scripts/ops/run_bitcoin_core_5module_research.sh          # local node
BASE=https://hackme.tech ADMIN_FILE=.secrets/... bash scripts/ops/run_bitcoin_core_5module_research.sh
```

Per module: small useful-PoW order + **property** fuzz campaign (`budget_runs=80`) on the linked WASM.

## Expected honest outcome

- **`script_push_bounds_guard` / `cpp_script_push_bounds_guard`:** fuzz sample should stay **clean** on a correct guard (violation class is rare in random inputs).
- **`bounds_guard` / `overflow_guard`:** property fuzz may report many `check returned 0` — that means “input not in accepting set”, **not** “RCE in Bitcoin Core”.
- **No `critical` sandbox crashes** on the five modules → post title: *“5 Core-inspired modules fuzzed — no exploitable WASM traps in this campaign.”*

## Public wording

- ✅ “Inspired by Bitcoin Core `script.cpp` / `tx_check.cpp` / `validation.cpp`”
- ✅ “Useful-PoW + property fuzz on HackMe open stack”
- ❌ “We found N bugs in Bitcoin Core” without maintainer reproduction on `bitcoind`

## Artifacts

- Run summary: `reports/bitcoin-core-5module/summary.json`
- Public report: https://hackme.tech/reports/bitcoin-core-5module.html
