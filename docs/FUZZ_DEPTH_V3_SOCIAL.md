# Fuzz Depth v3 — social copy (2026-06-25 · rc11o)

Live demo: `wasm_native` · duplicate-inputs guard · **4 guard signals → 4 native_confirmed** · 0 critical · verdict `fail_high`

---

## Telegram

```
🧪 Fuzz Depth v3 — WASM → Native → Bounty

Мы не переписывали экономику. Те же HMC, тот же escrow 20/80, тот же путь #orders → #fuzz.

Добавили три уровня глубины:

① wasm_only — 1 HMC · 64 runs
   Быстрый WASM-скан. Сигнал guard = finding.

② wasm_native — 5 HMC · 256 runs
   WASM ловит кандидатов → native bridge подтверждает или отклоняет на pinned Bitcoin guards.
   Bounty только при native_confirmed.

③ bytes_corpus — 10 HMC · 1000 runs
   Структурные byte-входы (tx/script class) через check_bytes(ptr,len).

━━━━━━━━━━━━━━━━━━━━
📊 Live demo (сегодня)
Guard: bitcoin_tx_dup_inputs (CVE-2018-17144 class)
4 guard signals · 4 native_confirmed · 0 critical

Майнерам: fuzz marketplace в #mining — видно per_run_hmc и активные кампании.

━━━━━━━━━━━━━━━━━━━━
📎 Отчёт: hackme.tech/reports/fuzz-depth-v3.html
🔧 Gate: bash scripts/ops/fuzz_depth_v3_gate.sh

Честный scope: Go-порты upstream guards, не полный bitcoind / libFuzzer — но инфраструктура (очередь, gate, UI, pool) уже production-ready.
```

---

## X (Twitter) — thread

**Tweet 1 / 7** — hook
```
🧪 Fuzz Depth v3 is live on HackMe

Same HMC economics. Same #orders → #fuzz flow.

What's new: WASM finds candidates → native bridge confirms or rejects → bounty only on deep tiers when native_confirmed.

Thread 🧵👇
```

**Tweet 2 / 7** — tiers
```
Three depth tiers in #orders:

wasm_only     →  1 HMC ·  64 runs   (WASM scan)
wasm_native   →  5 HMC · 256 runs   (+ native repro)
bytes_corpus  → 10 HMC · 1000 runs  (+ byte inputs)

Pick depth = pick budget. No tokenomics rewrite.
```

**Tweet 3 / 7** — native bridge
```
The native bridge:

WASM guard trip
    ↓
fuzz_native_queue
    ↓
confirmed ✅  or  rejected ❌
    ↓
bounty gate (deep tiers only)

Pinned Go ports of Bitcoin Core guards — honest scope, not full bitcoind yet.
```

**Tweet 4 / 7** — miners
```
Miners: fuzz is finally visible.

#mining → fuzz marketplace
• per_run_hmc
• active campaigns
• native_status column in #fuzz

Same pool workers, same escrow — now you see what pays.
```

**Tweet 5 / 7** — live numbers
```
Live demo today (wasm_native tier):

Guard: duplicate inputs (CVE-2018-17144 class)
4 guard signals
4 native_confirmed
0 critical
verdict: fail_high

WASM → native pipeline working end-to-end.
```

**Tweet 6 / 7** — reproduce
```
Reproduce locally:

bash scripts/ops/fuzz_depth_v3_gate.sh
POOL_DIST=0 bash scripts/ops/run_fuzz_depth_v3_live.sh

Byte corpus tier:
DEPTH_TIER=bytes_corpus BUDGET_RUNS=128 bash scripts/ops/run_fuzz_depth_v3_live.sh
```

**Tweet 7 / 7** — CTA + links
```
Full report + tiers breakdown:

→ hackme.tech/reports/fuzz-depth-v3.html
→ hackme.tech/#mining (marketplace)
→ github.com/jokeez/hackme

rc11o · Fuzz Depth v3
```

---

## X — single post (optional, if not threading)

```
Fuzz Depth v3 on HackMe rc11o 🧪

3 depth tiers · native_confirmed bounty gate · miner fuzz marketplace

Live: 4 signals → 4 native_confirmed on duplicate-inputs guard

hackme.tech/reports/fuzz-depth-v3.html
```

---

## News site

Item `2026-06-25-fuzz-depth-v3` in `web/site/assets/news.json` (top of feed).
