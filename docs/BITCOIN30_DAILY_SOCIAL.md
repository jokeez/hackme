# Bitcoin30 — 30 daily posts (Telegram + X)

Публиковать **по одному посту в день** — не заливать всё разом.

---

## Календарь публикации (пример)

| День серии | День поста | Фокус |
|------------|------------|-------|
| Day 01 | День 1 | GetScriptOp · 520 B push limit |
| Day 02 | День 2 | HasValidOps · invalid opcode scan |
| Day 03 | День 3 | CheckTransaction · MoneyRange |
| Day 04 | День 4 | Duplicate inputs · CVE-2018-17144 c |
| Day 05 | День 5 | EvalScript · push size 520 B |
| Day 06 | День 6 | SegWit witness stack |
| Day 07 | День 7 | EvalScript · op count 201 |
| Day 08 | День 8 | EvalScript · stack 1000 elements |
| Day 09 | День 9 | Block weight · 4M WU |
| Day 10 | День 10 | BIP34 · coinbase height |
| Day 11 | День 11 | Duplicate inputs · deep pass |
| Day 12 | День 12 | EvalScript stack · deep pass |
| Day 13 | День 13 | Witness stack · deep pass |
| Day 14 | День 14 | Op count · deep pass |
| Day 15 | День 15 | Duplicate inputs · wasm_native |
| Day 16 | День 16 | BIP34 · wasm_native |
| Day 17 | День 17 | Block weight · wasm_native |
| Day 18 | День 18 | EvalScript push · wasm_native |
| Day 19 | День 19 | Witness stack · wasm_native deep |
| Day 20 | День 20 | EvalScript stack · wasm_native deep |
| Day 21 | День 21 | Op count · wasm_native capstone |
| Day 22 | День 22 | Duplicate inputs · bytes_corpus |
| Day 23 | День 23 | BIP34 · bytes_corpus |
| Day 24 | День 24 | Block weight · bytes_corpus |
| Day 25 | День 25 | GetScriptOp · bytes_corpus |
| Day 26 | День 26 | HasValidOps · bytes_corpus |
| Day 27 | День 27 | MoneyRange · bytes_corpus |
| Day 28 | День 28 | EvalScript push · bytes deep |
| Day 29 | День 29 | Witness stack · bytes capstone |
| Day 30 | День 30 | EvalScript stack · Day 30 milestone |

**Week ledger posts** (опционально в конце недели): Day 7, 14, 21, 30.

---

## Общее мнение (редакционная линия)

**Что продаём честно:** не «нашли CVE в Bitcoin Core», а **ежедневный upstream guard fuzz** с прозрачными цифрами и 0 critical за 30 дней.

**Арка серии для аудитории:**
- **Week 1** — знакомство с 10 guards (первый проход)
- **Week 2** — deep passes, 128–256 runs
- **Week 3** — Fuzz Depth v3 `wasm_native` + native bridge (главный дифференциатор)
- **Week 4** — `bytes_corpus`, финал 30/30

**Лучшие дни для engagement:** 4, 11, 15 (dup inputs story), 3 и 27 (clean — контраст), 30 (finale).

**Не скрывать:** Days 18–21 — WASM signals без native_confirmed (per-guard native eval в roadmap). Это усиливает доверие.

**CTA:** всегда ссылка на day report + hub `bitcoin30.html`. С Week 3 — упоминать `fuzz-depth-v3.html`.


---


## Day 01 · GetScriptOp · 520 B push limit

**Week 1** · `wasm_only` · `upstream_bitcoin_getscriptop`

### Цифры

- Runs: **64**
- Guard signals: **59**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 1/30

GetScriptOp · 520 B push limit

📊 64 runs · 59 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Первый день серии — лимит элемента скрипта 520 байт.

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 1/30 — GetScriptOp

64 runs · 59 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 02 · HasValidOps · invalid opcode scan

**Week 1** · `wasm_only` · `upstream_bitcoin_hasvalidops`

### Цифры

- Runs: **64**
- Guard signals: **62**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 2/30

HasValidOps · invalid opcode scan

📊 64 runs · 62 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Проверка валидности opcode в CScript.

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 2/30 — HasValidOps

64 runs · 62 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 03 · CheckTransaction · MoneyRange

**Week 1** · `wasm_only` · `upstream_bitcoin_tx_check`

### Цифры

- Runs: **64**
- Guard signals: **0**
- Critical: **0** · Verdict: `clean`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 3/30

CheckTransaction · MoneyRange

📊 64 runs · CLEAN · 64 runs · 0 guard signals
Tier: wasm_only · verdict: clean

Единственный clean-модуль Week 1 — суммы в допустимом диапазоне.

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 3/30 — CheckTransaction

64 runs · CLEAN module · 0 guard signals

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing #security
```

---


## Day 04 · Duplicate inputs · CVE-2018-17144 class

**Week 1** · `wasm_only` · `upstream_bitcoin_tx_dup_inputs`

### Цифры

- Runs: **64**
- Guard signals: **5**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 4/30

Duplicate inputs · CVE-2018-17144 class

📊 64 runs · 5 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Классический guard на двойной prevout — исторический CVE-2018-17144.

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 4/30 — Duplicate inputs

64 runs · 5 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 05 · EvalScript · push size 520 B

**Week 1** · `wasm_only` · `upstream_bitcoin_evalscript_push`

### Цифры

- Runs: **64**
- Guard signals: **59**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 5/30

EvalScript · push size 520 B

📊 64 runs · 59 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

SCRIPT_ERR_PUSH_SIZE — oversized push в скрипте.

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 5/30 — EvalScript

64 runs · 59 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 06 · SegWit witness stack

**Week 1** · `wasm_only` · `upstream_bitcoin_witness_stack`

### Цифры

- Runs: **128**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 6/30

SegWit witness stack

📊 128 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Лимит push в witness stack (128 runs).

📎 https://hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 6/30 — SegWit witness stack

128 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 07 · EvalScript · op count 201

**Week 1** · `wasm_only` · `upstream_bitcoin_evalscript_opcount`

### Цифры

- Runs: **64**
- Guard signals: **56**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-week1.html

### Telegram

```
⛓️ Bitcoin30 · Day 7/30

EvalScript · op count 201

📊 64 runs · 56 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Week 1 finale — лимит non-push opcodes.

📎 https://hackme.tech/reports/bitcoin30-week1.html
✅ Week 1 complete → hackme.tech/reports/bitcoin30-week1.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 7/30 — EvalScript

64 runs · 56 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-week1.html

#bitcoin #fuzzing
```

---


## Day 08 · EvalScript · stack 1000 elements

**Week 2** · `wasm_only` · `upstream_bitcoin_evalscript_stack`

### Цифры

- Runs: **64**
- Guard signals: **64**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day08.html

### Telegram

```
⛓️ Bitcoin30 · Day 8/30

EvalScript · stack 1000 elements

📊 64 runs · 64 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Старт Week 2 — stack + altstack лимит.

📎 https://hackme.tech/reports/bitcoin30-day08.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 8/30 — EvalScript

64 runs · 64 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day08.html

#bitcoin #fuzzing
```

---


## Day 09 · Block weight · 4M WU

**Week 2** · `wasm_only` · `upstream_bitcoin_block_weight`

### Цифры

- Runs: **128**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day09.html

### Telegram

```
⛓️ Bitcoin30 · Day 9/30

Block weight · 4M WU

📊 128 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Consensus-adjacent — вес блока.

📎 https://hackme.tech/reports/bitcoin30-day09.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 9/30 — Block weight

128 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day09.html

#bitcoin #fuzzing
```

---


## Day 10 · BIP34 · coinbase height

**Week 2** · `wasm_only` · `upstream_bitcoin_coinbase_bip34`

### Цифры

- Runs: **128**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day10.html

### Telegram

```
⛓️ Bitcoin30 · Day 10/30

BIP34 · coinbase height

📊 128 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Coinbase обязан пушить высоту блока.

📎 https://hackme.tech/reports/bitcoin30-day10.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 10/30 — BIP34

128 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day10.html

#bitcoin #fuzzing
```

---


## Day 11 · Duplicate inputs · deep pass

**Week 2** · `wasm_only` · `upstream_bitcoin_tx_dup_inputs`

### Цифры

- Runs: **256**
- Guard signals: **24**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day11.html

### Telegram

```
⛓️ Bitcoin30 · Day 11/30

Duplicate inputs · deep pass

📊 256 runs · 24 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

256 runs — тот же guard что Day 4, 4× budget.

📎 https://hackme.tech/reports/bitcoin30-day11.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 11/30 — Duplicate inputs

256 runs · 24 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day11.html

#bitcoin #fuzzing
```

---


## Day 12 · EvalScript stack · deep pass

**Week 2** · `wasm_only` · `upstream_bitcoin_evalscript_stack`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day12.html

### Telegram

```
⛓️ Bitcoin30 · Day 12/30

EvalScript stack · deep pass

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

256 runs на stack guard.

📎 https://hackme.tech/reports/bitcoin30-day12.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 12/30 — EvalScript stack

256 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day12.html

#bitcoin #fuzzing
```

---


## Day 13 · Witness stack · deep pass

**Week 2** · `wasm_only` · `upstream_bitcoin_witness_stack`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day13.html

### Telegram

```
⛓️ Bitcoin30 · Day 13/30

Witness stack · deep pass

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

256 runs SegWit witness.

📎 https://hackme.tech/reports/bitcoin30-day13.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 13/30 — Witness stack

256 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day13.html

#bitcoin #fuzzing
```

---


## Day 14 · Op count · deep pass

**Week 2** · `wasm_only` · `upstream_bitcoin_evalscript_opcount`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day14.html

### Telegram

```
⛓️ Bitcoin30 · Day 14/30

Op count · deep pass

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_only · verdict: fail_high

Week 2 capstone — 256 runs opcode budget.

📎 https://hackme.tech/reports/bitcoin30-day14.html
✅ Week 2 complete → hackme.tech/reports/bitcoin30-week2.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 14/30 — Op count

256 runs · 80 guard signals · 0 critical
Tier: wasm_only

https://hackme.tech/reports/bitcoin30-day14.html

#bitcoin #fuzzing
```

---


## Day 15 · Duplicate inputs · wasm_native

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_tx_dup_inputs`

### Цифры

- Runs: **256**
- Guard signals: **24**
- Native confirmed: **24**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day15.html

### Telegram

```
⛓️ Bitcoin30 · Day 15/30

Duplicate inputs · wasm_native

📊 256 runs · 24 signals · native_confirmed: 24 · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native_confirmed: 24

Старт Week 3 — WASM → native bridge. 24/24 confirmed.

📎 https://hackme.tech/reports/bitcoin30-day15.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 15/30 — Duplicate inputs

256 runs · 24 guard signals · 24 native_confirmed · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day15.html

#bitcoin #fuzzing

First wasm_native day — WASM → native bridge live on HackMe.
```

---


## Day 16 · BIP34 · wasm_native

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_coinbase_bip34`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **81**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day16.html

### Telegram

```
⛓️ Bitcoin30 · Day 16/30

BIP34 · wasm_native

📊 256 runs · 80 signals · native_confirmed: 81 · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native_confirmed: 81

Native bridge на coinbase height guard.

📎 https://hackme.tech/reports/bitcoin30-day16.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 16/30 — BIP34

256 runs · 80 guard signals · 81 native_confirmed · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day16.html

#bitcoin #fuzzing
```

---


## Day 17 · Block weight · wasm_native

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_block_weight`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **81**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day17.html

### Telegram

```
⛓️ Bitcoin30 · Day 17/30

Block weight · wasm_native

📊 256 runs · 80 signals · native_confirmed: 81 · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native_confirmed: 81

4M WU guard под native repro.

📎 https://hackme.tech/reports/bitcoin30-day17.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 17/30 — Block weight

256 runs · 80 guard signals · 81 native_confirmed · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day17.html

#bitcoin #fuzzing
```

---


## Day 18 · EvalScript push · wasm_native

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_evalscript_push`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **0**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day18.html

### Telegram

```
⛓️ Bitcoin30 · Day 18/30

EvalScript push · wasm_native

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native: pending per-guard port (WASM signals only)

Push-size guard — WASM signals, per-guard native eval TBD.

📎 https://hackme.tech/reports/bitcoin30-day18.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 18/30 — EvalScript push

256 runs · 80 guard signals · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day18.html

#bitcoin #fuzzing
```

---


## Day 19 · Witness stack · wasm_native deep

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_witness_stack`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **0**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day19.html

### Telegram

```
⛓️ Bitcoin30 · Day 19/30

Witness stack · wasm_native deep

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native: pending per-guard port (WASM signals only)

Deep pass witness под v3 tier.

📎 https://hackme.tech/reports/bitcoin30-day19.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 19/30 — Witness stack

256 runs · 80 guard signals · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day19.html

#bitcoin #fuzzing
```

---


## Day 20 · EvalScript stack · wasm_native deep

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_evalscript_stack`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **0**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day20.html

### Telegram

```
⛓️ Bitcoin30 · Day 20/30

EvalScript stack · wasm_native deep

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native: pending per-guard port (WASM signals only)

Stack limit deep — mid Week 3.

📎 https://hackme.tech/reports/bitcoin30-day20.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 20/30 — EvalScript stack

256 runs · 80 guard signals · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day20.html

#bitcoin #fuzzing
```

---


## Day 21 · Op count · wasm_native capstone

**Week 3 · Fuzz v3** · `wasm_native` · `upstream_bitcoin_evalscript_opcount`

### Цифры

- Runs: **256**
- Guard signals: **80**
- Native confirmed: **0**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day21.html

### Telegram

```
⛓️ Bitcoin30 · Day 21/30

Op count · wasm_native capstone

📊 256 runs · 80 guard signals · 0 critical
Tier: wasm_native · verdict: fail_high
🔗 native: pending per-guard port (WASM signals only)

Week 3 finale — opcode budget.

📎 https://hackme.tech/reports/bitcoin30-day21.html
✅ Week 3 complete → hackme.tech/reports/bitcoin30-week3.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 21/30 — Op count

256 runs · 80 guard signals · 0 critical
Tier: wasm_native

https://hackme.tech/reports/bitcoin30-day21.html

#bitcoin #fuzzing
```

---


## Day 22 · Duplicate inputs · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_tx_dup_inputs`

### Цифры

- Runs: **512**
- Guard signals: **80**
- Native confirmed: **504**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day22.html

### Telegram

```
⛓️ Bitcoin30 · Day 22/30

Duplicate inputs · bytes_corpus

📊 512 runs · 80 signals · native_confirmed: 504 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 504

Week 4 — structured byte corpus mode.

📎 https://hackme.tech/reports/bitcoin30-day22.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 22/30 — Duplicate inputs

512 runs · 80 guard signals · 504 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day22.html

#bitcoin #fuzzing

Week 4: bytes_corpus tier — structured tx/script inputs.
```

---


## Day 23 · BIP34 · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_coinbase_bip34`

### Цифры

- Runs: **512**
- Guard signals: **80**
- Native confirmed: **4**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day23.html

### Telegram

```
⛓️ Bitcoin30 · Day 23/30

BIP34 · bytes_corpus

📊 512 runs · 80 signals · native_confirmed: 4 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 4

Byte inputs на coinbase guard.

📎 https://hackme.tech/reports/bitcoin30-day23.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 23/30 — BIP34

512 runs · 80 guard signals · 4 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day23.html

#bitcoin #fuzzing
```

---


## Day 24 · Block weight · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_block_weight`

### Цифры

- Runs: **512**
- Guard signals: **80**
- Native confirmed: **10**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day24.html

### Telegram

```
⛓️ Bitcoin30 · Day 24/30

Block weight · bytes_corpus

📊 512 runs · 80 signals · native_confirmed: 10 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 10

Byte corpus на block weight.

📎 https://hackme.tech/reports/bitcoin30-day24.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 24/30 — Block weight

512 runs · 80 guard signals · 10 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day24.html

#bitcoin #fuzzing
```

---


## Day 25 · GetScriptOp · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_getscriptop`

### Цифры

- Runs: **512**
- Guard signals: **80**
- Native confirmed: **362**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day25.html

### Telegram

```
⛓️ Bitcoin30 · Day 25/30

GetScriptOp · bytes_corpus

📊 512 runs · 80 signals · native_confirmed: 362 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 362

Новый модуль в byte mode — GetScriptOp.

📎 https://hackme.tech/reports/bitcoin30-day25.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 25/30 — GetScriptOp

512 runs · 80 guard signals · 362 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day25.html

#bitcoin #fuzzing
```

---


## Day 26 · HasValidOps · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_hasvalidops`

### Цифры

- Runs: **512**
- Guard signals: **80**
- Native confirmed: **413**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day26.html

### Telegram

```
⛓️ Bitcoin30 · Day 26/30

HasValidOps · bytes_corpus

📊 512 runs · 80 signals · native_confirmed: 413 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 413

HasValidOps под byte corpus.

📎 https://hackme.tech/reports/bitcoin30-day26.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 26/30 — HasValidOps

512 runs · 80 guard signals · 413 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day26.html

#bitcoin #fuzzing
```

---


## Day 27 · MoneyRange · bytes_corpus

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_tx_check`

### Цифры

- Runs: **512**
- Guard signals: **0**
- Native confirmed: **0**
- Critical: **0** · Verdict: `clean`
- Report: https://hackme.tech/reports/bitcoin30-day27.html

### Telegram

```
⛓️ Bitcoin30 · Day 27/30

MoneyRange · bytes_corpus

📊 512 runs · CLEAN · 512 runs · 0 guard signals
Tier: bytes_corpus · verdict: clean
🔗 native: pending per-guard port (WASM signals only)

Второй clean day серии — MoneyRange.

📎 https://hackme.tech/reports/bitcoin30-day27.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 27/30 — MoneyRange

512 runs · CLEAN module · 0 guard signals

https://hackme.tech/reports/bitcoin30-day27.html

#bitcoin #fuzzing #security
```

---


## Day 28 · EvalScript push · bytes deep

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_evalscript_push`

### Цифры

- Runs: **728**
- Guard signals: **80**
- Native confirmed: **518**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day28.html

### Telegram

```
⛓️ Bitcoin30 · Day 28/30

EvalScript push · bytes deep

📊 728 runs · 80 signals · native_confirmed: 518 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 518

Deep byte pass — push guard.

📎 https://hackme.tech/reports/bitcoin30-day28.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 28/30 — EvalScript push

728 runs · 80 guard signals · 518 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day28.html

#bitcoin #fuzzing
```

---


## Day 29 · Witness stack · bytes capstone

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_witness_stack`

### Цифры

- Runs: **600**
- Guard signals: **80**
- Native confirmed: **456**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day29.html

### Telegram

```
⛓️ Bitcoin30 · Day 29/30

Witness stack · bytes capstone

📊 600 runs · 80 signals · native_confirmed: 456 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 456

Witness byte corpus capstone.

📎 https://hackme.tech/reports/bitcoin30-day29.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 29/30 — Witness stack

600 runs · 80 guard signals · 456 native_confirmed · 0 critical
Tier: bytes_corpus

https://hackme.tech/reports/bitcoin30-day29.html

#bitcoin #fuzzing
```

---


## Day 30 · EvalScript stack · Day 30 milestone

**Week 4 · bytes** · `bytes_corpus` · `upstream_bitcoin_evalscript_stack`

### Цифры

- Runs: **760**
- Guard signals: **80**
- Native confirmed: **393**
- Critical: **0** · Verdict: `fail_high`
- Report: https://hackme.tech/reports/bitcoin30-day30.html

### Telegram

```
⛓️ Bitcoin30 · Day 30/30

EvalScript stack · Day 30 milestone

📊 760 runs · 80 signals · native_confirmed: 393 · 0 critical
Tier: bytes_corpus · verdict: fail_high
🔗 native_confirmed: 393

30/30 complete — stack guard, серия закрыта.

📎 https://hackme.tech/reports/bitcoin30-day30.html
🏁 Серия 30/30 complete → hackme.tech/reports/bitcoin30.html

Honest scope: WASM guard excerpt — not full bitcoind.
```

### X (Twitter)

```
Bitcoin30 Day 30/30 — SERIES COMPLETE 🏁

760 runs · 80 signals · 8,808 total runs · 0 critical across 30 days

https://hackme.tech/reports/bitcoin30-day30.html

hackme.tech/reports/bitcoin30.html
```

---
