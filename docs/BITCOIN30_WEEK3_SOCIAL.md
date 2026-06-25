# Bitcoin30 Week 3 — social copy (days 15–21 · wasm_native)

> **Полный календарь 30 дней:** см. [`BITCOIN30_DAILY_SOCIAL.md`](./BITCOIN30_DAILY_SOCIAL.md) — Telegram + X на каждый день.

**Week 3 totals:** 1,792 runs · 504 guard signals · 186 native_confirmed · **0 critical**

---

## Week 3 summary (Telegram)

```
✅ Bitcoin30 Week 3 DONE — Fuzz Depth v3 on Bitcoin Core

Days 15–21 · wasm_native tier (5 HMC · 256 runs)
1,792 runs · 504 guard signals · 186 native_confirmed · 0 critical

15 · duplicate inputs + native bridge — 24 → 24 confirmed
16 · BIP34 coinbase height — 80 → 81 confirmed
17 · block weight 4M WU — 80 → 81 confirmed
18 · EvalScript push size — 80 signals
19 · SegWit witness stack deep — 80 signals
20 · EvalScript stack deep — 80 signals
21 · EvalScript op count capstone — 80 signals

Ledger → hackme.tech/reports/bitcoin30-week3.html
Series → hackme.tech/reports/bitcoin30.html

Honest scope: WASM + Go native ports. Days 18–21 use generic native port until per-guard eval lands.
```

---

## Week 3 summary (X thread)

**1/5** Bitcoin30 Week 3 is complete — 7 days of Bitcoin Core fuzz with Fuzz Depth v3 wasm_native tier 🧵

**2/5** 1,792 runs · 504 guard signals · 186 native_confirmed · 0 critical across the week.

**3/5** Highlights: Day 15 dup-inputs (CVE-2018-17144 class) — 24 WASM signals, all 24 native_confirmed.

**4/5** BIP34 + block weight guards: 80 signals/day, native bridge confirmed on pinned logic.

**5/5** Full ledger: hackme.tech/reports/bitcoin30-week3.html · Week 4 → bytes_corpus days 22–30.

---

## Daily posts

### Day 15
**TG:** Day 15/30 — dup inputs wasm_native · 24 signals · 24 native_confirmed · hackme.tech/reports/bitcoin30-day15.html
**X:** Bitcoin30 D15: duplicate inputs + native bridge. 24/24 confirmed. 0 critical.

### Day 16
**TG:** Day 16/30 — BIP34 wasm_native · 80 signals · 81 native_confirmed
**X:** D16 BIP34 coinbase height — wasm_native tier live.

### Day 17
**TG:** Day 17/30 — block weight wasm_native · 80 signals · 81 native_confirmed
**X:** D17 MAX_BLOCK_WEIGHT guard under native bridge.

### Day 18
**TG:** Day 18/30 — EvalScript push wasm_native · 80 signals · 0 critical
**X:** D18 SCRIPT_ERR_PUSH_SIZE — Week 3 mid-week.

### Day 19
**TG:** Day 19/30 — witness stack deep wasm_native · 80 signals
**X:** D19 SegWit witness stack deep pass.

### Day 20
**TG:** Day 20/30 — EvalScript stack deep · 80 signals
**X:** D20 stack+altstack 1000-element limit fuzz.

### Day 21 · Week 3 capstone
**TG:** Week 3 DONE ✅ Day 21 op-count capstone · ledger bitcoin30-week3.html
**X:** Bitcoin30 Week 3 complete. 7 modules · 0 critical. Next: bytes_corpus Week 4.

---

## Reproduce

```bash
DAY=15 bash scripts/ops/run_bitcoin30_day.sh   # auto wasm_native for days 15–21
bash scripts/ops/run_bitcoin30_week3.sh
python3 scripts/ops/export_bitcoin30_day_html.py 15 16 17 18 19 20 21
python3 scripts/ops/export_bitcoin30_week_html.py 3
```
