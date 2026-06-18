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

🔗 hackme.tech/reports/bitcoin30-day10.html (after deploy)
#HackMe #Bitcoin #BIP34

### X

Day 10/30 — Bitcoin Core BIP34 coinbase height guard on HackMe WASM fuzz.

128 runs on upstream_bitcoin_coinbase_bip34.wasm — honest guard-signal triage, not a CVE claim.

#Bitcoin #Fuzzing

---

## Day 11 — Duplicate inputs deep pass (published 18 Jun 2026)

### Telegram

🧪 Day 11/30 — Deep pass: CheckTransaction duplicate inputs (CVE-2018-17144 *class*).

Live run:
• 256/256 runs · ~106s
• 0 critical · 24 guard signals (duplicate-input class rejected)
• 165 new edges · 199 paths

🔗 hackme.tech/reports/bitcoin30-day11.html (after deploy)

256 runs · same guard as Day 4, higher budget — hunting edge cases in prevout dedup.

0 critical = good. Guard signals = filter doing its job.

#HackMe #Bitcoin

### X

Day 11/30 — 256-run deep fuzz on Bitcoin Core duplicate-input guard (CVE-2018-17144 class). Guard signals ≠ bounty. #Bitcoin #Fuzzing

---

## Day 12 — EvalScript stack deep pass

### Telegram

🧪 Day 12/30 — Deep pass: EvalScript MAX_STACK_SIZE (1000 elements).

256 runs on interpreter stack guard. Week-2 focus: script resource limits.

#HackMe #Bitcoin

### X

Day 12/30 — deep fuzz Bitcoin Core EvalScript stack-size guard. 256 runs · WASM guard · no CVE claim without native repro. #Fuzzing

---

## Day 13 — Witness stack deep pass

### Telegram

🧪 Day 13/30 — Deep pass: SegWit witness stack push cap.

256 runs · VerifyWitnessProgram-inspired guard.

#HackMe #Bitcoin #SegWit

### X

Day 13/30 — SegWit witness stack guard, 256-run deep pass on HackMe. #Bitcoin #SecurityResearch

---

## Day 14 — Op count deep pass

### Telegram

🧪 Day 14/30 — Deep pass: EvalScript SCRIPT_ERR_OP_COUNT (201 ops).

256 runs · closes week-2 block. Week 1 ledger: 576 runs · 0 critical.

Pilot: 5 free fuzz slots — GitHub issue `fuzz-pilot`.

🔗 hackme.tech/reports/bitcoin30-week1.html
#HackMe #Bitcoin

### X

Day 14/30 — Bitcoin Core opcode-count guard, 256-run deep pass. Week 2 block done · 0 critical across series so far. hackme.tech #Fuzzing

---

## Posting schedule (suggested)

| Calendar | Day | Action |
|----------|-----|--------|
| 18 Jun | 9 | Post Day 9 TG + X (report live) |
| 19 Jun | 10 | Run DAY=10 · post after DAY_SUMMARY |
| 20 Jun | 11 | Deep dup-inputs |
| 21 Jun | 12 | Deep stack |
| 22 Jun | 13 | Deep witness |
| 23 Jun | 14 | Deep op-count · week-2 recap thread |
