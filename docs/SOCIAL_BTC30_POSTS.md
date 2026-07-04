# HackMe · Social posts — Bitcoin Core 30-Day Fuzz (Days 1–30)

**Series hub:** https://hackme.tech/reports/bitcoin30.html  
**Network:** https://hackme.tech/  
**Channels:** Telegram (main) · Twitter / X

Honest scope: WASM upstream guards on useful-PoW — not full `bitcoind` differential fuzz. Guard signals ≠ CVE claims.

---

## How to use

- Post **one day at a time** (or catch-up batches of 3–5).
- **Twitter:** main post; optional reply: `WASM excerpt · detector semantics · not a CVE claim`.
- **Telegram:** full post with stats + link.

---

## Series kickoff (before Day 1)

### Twitter / X

```
30 days · Bitcoin Core upstream WASM fuzz 🔬⛏️

Not empty hashes — useful-PoW on HackMe:
real guards from bitcoin/bitcoin · daily ledgers · honest triage

Day 1 → GetScriptOp 520B cap
https://hackme.tech/reports/bitcoin30.html

#Bitcoin #BitcoinCore #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core 30-Day Fuzz — СТАРТ**

**30 дней** upstream WASM-фазза **bitcoin/bitcoin** на пуле HackMe — полезный PoW, не лотерея хешей.

📅 Week 1–4 · detector semantics · **0 CVE без repro**
📚 https://hackme.tech/reports/bitcoin30.html
⛏️ https://hackme.tech/downloads.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 1/30 — GetScriptOp · MAX_SCRIPT_ELEMENT_SIZE

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 1/30 · Bitcoin Core WASM fuzz 🚀

GetScriptOp · MAX_SCRIPT_ELEMENT_SIZE
64 runs · 59 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 1/30 — GetScriptOp · MAX_SCRIPT_ELEMENT_SIZE** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• 59 guard signals · **0 critical**
• Focus: 520-byte push cap

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 2/30 — CScript::HasValidOps()

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 2/30 · Bitcoin Core WASM fuzz 🚀

CScript::HasValidOps()
64 runs · 62 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 2/30 — CScript::HasValidOps()** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• 62 guard signals · **0 critical**
• Focus: invalid opcode filter

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 3/30 — CheckTransaction · MoneyRange

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 3/30 · Bitcoin Core WASM fuzz 🚀

CheckTransaction · MoneyRange
64 runs · clean module ✨ · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 3/30 — CheckTransaction · MoneyRange** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• clean module ✨ · **0 critical**
• Focus: value sanity check

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 4/30 — Duplicate inputs (CVE-2018-17144 class)

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 4/30 · Bitcoin Core WASM fuzz 🚀

Duplicate inputs (CVE-2018-17144 class)
64 runs · 5 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #CVE
```

### Telegram

```
🔬 **Bitcoin Core · Day 4/30 — Duplicate inputs (CVE-2018-17144 class)** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• 5 guard signals · **0 critical**
• Focus: prevout dedup

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 5/30 — EvalScript · SCRIPT_ERR_PUSH_SIZE

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 5/30 · Bitcoin Core WASM fuzz 🚀

EvalScript · SCRIPT_ERR_PUSH_SIZE
64 runs · 59 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 5/30 — EvalScript · SCRIPT_ERR_PUSH_SIZE** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• 59 guard signals · **0 critical**
• Focus: 520 B push limit

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 6/30 — SegWit witness stack push cap

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 6/30 · Bitcoin Core WASM fuzz 🚀

SegWit witness stack push cap
128 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 6/30 — SegWit witness stack push cap** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 128 runs · `wasm_only`
• 80 guard signals · **0 critical**
• Focus: witness element size

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 7/30 — EvalScript · SCRIPT_ERR_OP_COUNT

**Week 1** · `wasm_only` · https://hackme.tech/reports/bitcoin30-week1.html

### Twitter / X

```
Day 7/30 · Bitcoin Core WASM fuzz 🚀

EvalScript · SCRIPT_ERR_OP_COUNT
64 runs · 56 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-week1.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 7/30 — EvalScript · SCRIPT_ERR_OP_COUNT** 🚀

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `wasm_only`
• 56 guard signals · **0 critical**
• Focus: 201-op budget

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-week1.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 8/30 — EvalScript · SCRIPT_ERR_STACK_SIZE

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day08.html

### Twitter / X

```
Day 8/30 · Bitcoin Core WASM fuzz 🔥

EvalScript · SCRIPT_ERR_STACK_SIZE
64 runs · 64 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day08.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 8/30 — EvalScript · SCRIPT_ERR_STACK_SIZE** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 64 runs · `deep pass`
• 64 guard signals · **0 critical**
• Focus: 1000-element stack

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day08.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 9/30 — Block weight · MAX_BLOCK_WEIGHT

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day09.html

### Twitter / X

```
Day 9/30 · Bitcoin Core WASM fuzz 🔥

Block weight · MAX_BLOCK_WEIGHT
128 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day09.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 9/30 — Block weight · MAX_BLOCK_WEIGHT** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 128 runs · `deep pass`
• 80 guard signals · **0 critical**
• Focus: 4M weight units

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day09.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 10/30 — BIP34 coinbase height push

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day10.html

### Twitter / X

```
Day 10/30 · Bitcoin Core WASM fuzz 🔥

BIP34 coinbase height push
128 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day10.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 10/30 — BIP34 coinbase height push** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 128 runs · `deep pass`
• 80 guard signals · **0 critical**
• Focus: height in coinbase

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day10.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 11/30 — Duplicate inputs · deep pass

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day11.html

### Twitter / X

```
Day 11/30 · Bitcoin Core WASM fuzz 🔥

Duplicate inputs · deep pass
256 runs · 24 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day11.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #CVE
```

### Telegram

```
🔬 **Bitcoin Core · Day 11/30 — Duplicate inputs · deep pass** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `deep pass`
• 24 guard signals · **0 critical**
• Focus: CVE-2018-17144 class

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day11.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 12/30 — EvalScript stack · deep pass

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day12.html

### Twitter / X

```
Day 12/30 · Bitcoin Core WASM fuzz 🔥

EvalScript stack · deep pass
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day12.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 12/30 — EvalScript stack · deep pass** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `deep pass`
• 80 guard signals · **0 critical**
• Focus: combined stack depth

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day12.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 13/30 — SegWit witness stack · deep pass

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day13.html

### Twitter / X

```
Day 13/30 · Bitcoin Core WASM fuzz 🔥

SegWit witness stack · deep pass
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day13.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 13/30 — SegWit witness stack · deep pass** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `deep pass`
• 80 guard signals · **0 critical**
• Focus: VerifyWitnessProgram

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day13.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 14/30 — EvalScript op count · deep pass

**Week 2** · `deep pass` · https://hackme.tech/reports/bitcoin30-day14.html

### Twitter / X

```
Day 14/30 · Bitcoin Core WASM fuzz 🔥

EvalScript op count · deep pass
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day14.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security
```

### Telegram

```
🔬 **Bitcoin Core · Day 14/30 — EvalScript op count · deep pass** 🔥

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `deep pass`
• 80 guard signals · **0 critical**
• Focus: opcode budget

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day14.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 15/30 — Duplicate inputs

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day15.html

### Twitter / X

```
Day 15/30 · Bitcoin Core WASM fuzz ⚡

Duplicate inputs
256 runs · 24 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day15.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 15/30 — Duplicate inputs** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 24 guard signals · **0 critical**
• Focus: tx_check prevout dedup

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day15.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 16/30 — BIP34 coinbase height

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day16.html

### Twitter / X

```
Day 16/30 · Bitcoin Core WASM fuzz ⚡

BIP34 coinbase height
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day16.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 16/30 — BIP34 coinbase height** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: ConnectBlock rule

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day16.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 17/30 — Block weight

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day17.html

### Twitter / X

```
Day 17/30 · Bitcoin Core WASM fuzz ⚡

Block weight
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day17.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 17/30 — Block weight** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: MAX_BLOCK_WEIGHT

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day17.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 18/30 — EvalScript push size

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day18.html

### Twitter / X

```
Day 18/30 · Bitcoin Core WASM fuzz ⚡

EvalScript push size
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day18.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 18/30 — EvalScript push size** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: interpreter push cap

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day18.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 19/30 — SegWit witness stack

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day19.html

### Twitter / X

```
Day 19/30 · Bitcoin Core WASM fuzz ⚡

SegWit witness stack
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day19.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 19/30 — SegWit witness stack** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: VerifyWitnessProgram

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day19.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 20/30 — EvalScript stack size

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day20.html

### Twitter / X

```
Day 20/30 · Bitcoin Core WASM fuzz ⚡

EvalScript stack size
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day20.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 20/30 — EvalScript stack size** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: stack + altstack

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day20.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 21/30 — EvalScript op count

**Week 3** · `wasm_native` · https://hackme.tech/reports/bitcoin30-day21.html

### Twitter / X

```
Day 21/30 · Bitcoin Core WASM fuzz ⚡

EvalScript op count
256 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day21.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM
```

### Telegram

```
🔬 **Bitcoin Core · Day 21/30 — EvalScript op count** ⚡

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 256 runs · `wasm_native`
• 80 guard signals · **0 critical**
• Focus: nOpCount limit

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day21.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 22/30 — Duplicate inputs

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day22.html

### Twitter / X

```
Day 22/30 · Bitcoin Core WASM fuzz 🏁

Duplicate inputs
512 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day22.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 22/30 — Duplicate inputs** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: structured byte corpus

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day22.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 23/30 — BIP34

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day23.html

### Twitter / X

```
Day 23/30 · Bitcoin Core WASM fuzz 🏁

BIP34
512 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day23.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 23/30 — BIP34** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: coinbase height bytes

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day23.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 24/30 — Block weight

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day24.html

### Twitter / X

```
Day 24/30 · Bitcoin Core WASM fuzz 🏁

Block weight
512 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day24.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 24/30 — Block weight** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: weight budget bytes

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day24.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 25/30 — GetScriptOp

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day25.html

### Twitter / X

```
Day 25/30 · Bitcoin Core WASM fuzz 🏁

GetScriptOp
512 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day25.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 25/30 — GetScriptOp** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: script parsing

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day25.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 26/30 — HasValidOps

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day26.html

### Twitter / X

```
Day 26/30 · Bitcoin Core WASM fuzz 🏁

HasValidOps
512 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day26.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 26/30 — HasValidOps** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: opcode validity

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day26.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 27/30 — CheckTransaction MoneyRange

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day27.html

### Twitter / X

```
Day 27/30 · Bitcoin Core WASM fuzz 🏁

CheckTransaction MoneyRange
512 runs · clean module ✨ · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day27.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 27/30 — CheckTransaction MoneyRange** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 512 runs · `bytes_corpus`
• clean module ✨ · **0 critical**
• Focus: clean money range

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day27.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 28/30 — EvalScript push

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day28.html

### Twitter / X

```
Day 28/30 · Bitcoin Core WASM fuzz 🏁

EvalScript push
728 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day28.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 28/30 — EvalScript push** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 728 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: push size stress

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day28.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 29/30 — Witness stack

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day29.html

### Twitter / X

```
Day 29/30 · Bitcoin Core WASM fuzz 🏁

Witness stack
600 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day29.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore
```

### Telegram

```
🔬 **Bitcoin Core · Day 29/30 — Witness stack** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 600 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: witness depth

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day29.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Day 30/30 — EvalScript stack

**Week 4** · `bytes_corpus` · https://hackme.tech/reports/bitcoin30-day30.html

### Twitter / X

```
Day 30/30 · Bitcoin Core WASM fuzz 🏁

EvalScript stack
760 runs · 80 guard signals · 0 critical

Useful-PoW on HackMe — real consensus guards, not lottery mining.

👇 https://hackme.tech/reports/bitcoin30-day30.html

#Bitcoin #Fuzzing #HackMe #UsefulPoW #Security #WASM #BitcoinCore #Finale
```

### Telegram

```
🔬 **Bitcoin Core · Day 30/30 — EvalScript stack** 🏁

Фаззим **upstream WASM-гарды** bitcoin/bitcoin на useful-PoW пуле HackMe.

📊 **Статистика**
• 760 runs · `bytes_corpus`
• 80 guard signals · **0 critical**
• Focus: stack limits finale

⚠️ WASM excerpt · detector semantics · не CVE-заявление

📎 https://hackme.tech/reports/bitcoin30-day30.html
📚 https://hackme.tech/reports/bitcoin30.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #WASM #L1
```

---

## Series finale (after Day 30)

### Twitter / X

```
Day 30/30 DONE 🏁

Bitcoin Core 30-Day WASM fuzz complete on HackMe

8,808 runs · 1,953 guard signals · 0 critical
wasm_only → deep → wasm_native → bytes_corpus

https://hackme.tech/reports/bitcoin30.html

#Bitcoin #HackMe #Fuzzing #UsefulPoW #SecurityResearch
```

### Telegram

```
🏁 **Bitcoin Core 30-Day Fuzz — ФИНАЛ**

**30/30** на useful-PoW пуле HackMe.

📊 8,808 runs · 1,953 guard signals · **0 critical**

📎 https://hackme.tech/reports/bitcoin30-day30.html
📚 https://hackme.tech/reports/bitcoin30.html
⛏️ https://hackme.tech/downloads.html

#HackMe #Bitcoin #Fuzzing #UsefulPoW #Security #L1
```

---

*Operator pack · `DAY=N bash scripts/ops/run_bitcoin30_day.sh` · no CVE without independent confirmation.*
