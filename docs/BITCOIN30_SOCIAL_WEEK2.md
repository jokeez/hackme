# Bitcoin30 — social drafts (Week 2 · days 9–14)

Copy to Telegram / X after each live run. Update run stats from `reports/bitcoin30/CURRENT/DAY_SUMMARY.json`.
Local-only mirror: `docs/social/` (gitignored).

---

## Day 9 — Block weight (published 18 Jun 2026)

### Telegram

🧪 HackMe · Bitcoin Core 30-day fuzz — Day 9/30

Day 9 — SegWit block weight budget from Core consensus:

📌 Today: MAX_BLOCK_WEIGHT (4M WU) · GetBlockWeight
📌 Upstream: bitcoin/bitcoin → validation.h
📌 HackMe: tasks/sources/security/upstream/bitcoin_block_weight.c

Live run (local node):
• 128/128 runs · ~30s
• 0 critical · 80 guard signals (over-weight blocks rejected — filter working)
• 99 new edges · 110 paths

Honest triage: guard signals ≠ new CVE. WASM slice, not full node.

🔗 Report: hackme.tech/reports/bitcoin30-day09.html
#HackMe #Bitcoin #Fuzzing #BitcoinCore

### X

Day 9/30 — fuzzing a Bitcoin Core MAX_BLOCK_WEIGHT guard on @hackme open stack.

128 runs · 0 critical · 80 guard signals (expected rejections on over-weight inputs).

Report: hackme.tech/reports/bitcoin30-day09.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind.

#Bitcoin #Fuzzing #SecurityResearch

---

## Day 10 — BIP34 coinbase height (post after run)

### Telegram

🧪 HackMe · Bitcoin Core 30-day fuzz — Day 10/30

Day 10 — BIP34 coinbase height push:

📌 Today: coinbase must push block height (post-block 227,835)
📌 Upstream: bitcoin/bitcoin → validation.cpp ConnectBlock
📌 HackMe: tasks/sources/security/upstream/bitcoin_coinbase_bip34.c

Live run (local node):
• 128/128 runs · ~31s
• 0 critical · 80 guard signals (malformed coinbase — filter working)
• 99 new edges · 110 paths

Tomorrow (Day 11): duplicate-input deep pass (CVE-2018-17144 class).

🔗 hackme.tech/reports/bitcoin30-day10.html
#HackMe #Bitcoin #BIP34 #Fuzzing

### X

Day 10/30 — Bitcoin Core BIP34 coinbase height guard on HackMe WASM fuzz.

128 runs · 0 critical · 80 guard signals (malformed coinbase class).

Report: hackme.tech/reports/bitcoin30-day10.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind.

#Bitcoin #Fuzzing #SecurityResearch

---

## Day 11 — Duplicate inputs deep pass (published 18 Jun 2026)

### Telegram

🧪 Day 11/30 — Deep pass: CheckTransaction duplicate inputs (CVE-2018-17144 *class*).

Live run:
• 256/256 runs · ~106s
• 0 critical · 24 guard signals (duplicate-input class rejected)
• 165 new edges · 199 paths

🔗 hackme.tech/reports/bitcoin30-day11.html

256 runs · same guard as Day 4, higher budget — hunting edge cases in prevout dedup.

0 critical = good. Guard signals = filter doing its job.

#HackMe #Bitcoin

### X

Day 11/30 — 256-run deep fuzz on Bitcoin Core duplicate-input guard (CVE-2018-17144 class). Guard signals ≠ bounty. #Bitcoin #Fuzzing

---

## Day 12 — EvalScript stack deep pass (published 20 Jun 2026)

### Telegram

🧪 HackMe · Bitcoin Core 30-day fuzz — Day 12/30

Day 12 — EvalScript stack + altstack size limit (deep pass):

📌 Today: SCRIPT_ERR_STACK_SIZE · MAX_STACK_SIZE (1000 elements)
📌 Upstream: bitcoin/bitcoin → interpreter.cpp EvalScript L334–335
📌 HackMe: tasks/sources/security/upstream/bitcoin_evalscript_stack.c
📌 Same guard as Day 8 — 4× run budget (256 vs 64)

Live run (local node · full HackMe audit pipeline):
• 256/256 runs · ~99s
• 0 critical · 80 guard signals (over-limit stack encodings rejected)
• 165 new edges · 199 paths · verdict fail_high (detector semantics)

Honest triage: guard signals = filter doing its job, not a new consensus bug. WASM slice — not bitcoind.

Series so far: Week 1 + days 8–12 → **0 critical** across all published runs.

🔗 Report: hackme.tech/reports/bitcoin30-day12.html
🔗 Hub: hackme.tech/reports/bitcoin30.html

Mine useful PoW on the same stack → hackme.tech/downloads.html

#HackMe #Bitcoin #Fuzzing #BitcoinCore #SecurityResearch

### X

Day 12/30 — deep fuzz on a Bitcoin Core EvalScript MAX_STACK_SIZE guard (1000-element stack cap).

256 runs · 0 critical · 80 guard signals on reject-path inputs · ~99s on @hackme open stack (escrow + WASM sandbox + public report).

Same module as Day 8, higher budget — hunting edge cases in script resource limits.

Report: hackme.tech/reports/bitcoin30-day12.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind. Guard signals ≠ bounty.

#Bitcoin #Fuzzing #SecurityResearch #OpenSource

---

## Day 13 — Witness stack deep pass

### Telegram

🧪 HackMe · Bitcoin Core 30-day fuzz — Day 13/30

Day 13 — SegWit witness stack push cap (deep pass):

📌 Today: VerifyWitnessProgram · SCRIPT_ERR_PUSH_SIZE on witness stack
📌 Upstream: bitcoin/bitcoin → interpreter.cpp witness validation
📌 HackMe: tasks/sources/security/upstream/bitcoin_witness_stack.c

256 runs · 80 guard signals · 0 critical · ~155s on HackMe open stack (escrow + WASM sandbox + public report).

Same module as Day 6, higher budget — hunting edge cases in witness resource limits.

Report: hackme.tech/reports/bitcoin30-day13.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind. Guard signals ≠ bounty.

Pilot: 5 free fuzz slots — GitHub issue `fuzz-pilot`.

#HackMe #Bitcoin #SegWit #Fuzzing #SecurityResearch

### X

Day 13/30 — deep fuzz on a Bitcoin Core SegWit witness stack guard (push-size cap).

256 runs · 0 critical · 80 guard signals on reject-path inputs · ~155s on @hackme open stack.

Same module as Day 6, higher budget — witness resource limits.

Report: hackme.tech/reports/bitcoin30-day13.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind.

#Bitcoin #Fuzzing #SegWit #SecurityResearch

---

## Day 14 — Op count deep pass · week 2 close

### Telegram

🧪 HackMe · Bitcoin Core 30-day fuzz — Day 14/30

Day 14 — EvalScript SCRIPT_ERR_OP_COUNT (201 ops · deep pass):

📌 Today: EvalScript nOpCount · opcode budget guard
📌 Upstream: bitcoin/bitcoin → interpreter.cpp L462-L463
📌 HackMe: tasks/sources/security/upstream/bitcoin_evalscript_opcount.c

256 runs · 80 guard signals · 0 critical · ~129s on HackMe open stack.

Week 2 block complete · two-week ledger: 1,856 runs · 809 guard signals · 0 critical.

Two-week report: hackme.tech/reports/bitcoin30-two-weeks.html
Day 14 report: hackme.tech/reports/bitcoin30-day14.html

Not a CVE claim — WASM guard inspired by Core, not bitcoind.

Pilot: 5 free fuzz slots — GitHub issue `fuzz-pilot`.

#HackMe #Bitcoin #Fuzzing #SecurityResearch

### X

Day 14/30 — Bitcoin Core opcode-count guard, 256-run deep pass. Week 2 block done.

256 runs · 0 critical · 80 guard signals · two-week total: 1,856 runs · 0 critical.

Two-week ledger: hackme.tech/reports/bitcoin30-two-weeks.html

#Bitcoin #Fuzzing #SecurityResearch
## OSS repo fuzz queue (next targets)

Rotate **1 Bitcoin30 day (15+)** + **1–2 OSS targets**. Write maintainers **only on critical** with raw repro.

| Priority | Repo | Why | HackMe path | Estimate |
|----------|------|-----|-------------|----------|
| A | [ckpool](https://github.com/kano1ckpool/ckpool) | Classic stratum proxy · high miner exposure | Map `client.c` / line parser → WASM guards (mkpool playbook) | **High** · 2–3 days pack + campaign |
| B | [stratum-mining/stratum](https://github.com/stratum-mining/stratum) | Sv2 reference · active spec work | Frame/codec bounds like WarpPool pass | **Medium** · guards exist, needs Sv2 wire map |
| C | [dogecoin/dogecoin](https://github.com/dogecoin/dogecoin) | Fork script guards already ported | `run_oss_pr_fuzz_hunt.sh` → `dogecoin_hasvalidops` | **Low effort** · WASM ready, 1 day |
| D | [mity/centijson](https://github.com/mity/centijson) | JSON nest-depth DoS · small surface | `upstream_centijson_nest_depth.wasm` ready | **Low effort** · quick win, non-Bitcoin |
| Skip | WarpPool, mkpool | Already deep-passed · clean · no spam | Reopen only with native repro | Done |

```bash
DAY=15 bash scripts/ops/run_bitcoin30_day.sh   # when Day 15 guard wired
bash scripts/ops/run_oss_pr_fuzz_hunt.sh       # pick from upstream/oss_pr_fuzz_queue.json
```

## Posting schedule (suggested)

Prefuzzed — post on your calendar; reports already live on hackme.tech.

| Calendar | Day | Action |
|----------|-----|--------|
| 18 Jun | 9 | Post Day 9 TG + X |
| 19 Jun | 10 | Post Day 10 TG + X |
| 20 Jun | 11–12 | Post Day 11–12 TG + X |
| 21 Jun | 13 | Post Day 13 TG + X · optional OSS fuzz (ckpool pack start) |
| 22 Jun | 14 | Post Day 14 TG + X · **two-week recap thread** → bitcoin30-two-weeks.html |
| 23+ | 15+ | Resume daily run + post · OSS rotation |
