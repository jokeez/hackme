# B2B Fuzz Final — social & site copy (July 2026)

Use after `hackme-fuzzing` wizard ships in dist + `docs/DEVELOPERS_FUZZING.md` is live on hackme.tech.

**News ID:** `2026-07-01-b2b-fuzz-final`  
**Publish:** `FORCE_NEWS_ID=2026-07-01-b2b-fuzz-final NODE_SSH=hackme-vps bash scripts/ops/publish_news_to_telegram.sh`

---

## X / Twitter (@HackMeTech)

### Single tweet (punchy)

```
Your WASM guard. One command. A report your CI can trust.

HackMe B2B fuzz final:
→ hackme-fuzzing wizard (Scan · Audit · Deep)
→ distributed pool + HMC escrow 20/80
→ GET /gate for CI — pass or block the merge

Not a dashboard toy. Useful-PoW security audits.

https://hackme.tech/developers.html
```

### Single tweet (RU)

```
Один guard.wasm → одна команда → отчёт + gate для CI.

HackMe B2B fuzz final:
wizard · Scan/Audit/Deep · pool · escrow 20/80

https://hackme.tech/developers.html
```

### Thread (EN) — 8 tweets

**1/8 — hook**
```
🛡️ We shipped B2B fuzz FINAL.

Upload a WASM security guard → pick Scan, Audit, or Deep → miners on the HackMe pool fuzz it → you get a report + a CI gate URL.

One CLI command. No browser admin on the public site.

Thread 🧵
```

**2/8 — the command**
```
$ hackme-fuzzing wizard \
    --wasm ./guard.wasm \
    --package audit \
    --title "My protocol guard"

→ campaign_id
→ customer_report_token (once — save it)
→ report.html · pulse · gate?pass=true

Local node pays escrow. hackme.tech delivers the report.
```

**3/8 — packages**
```
Three packages. No spreadsheet quotes.

Scan   · wasm_only    ·  1 HMC ·   64 runs · CI smoke
Audit  · wasm_native  ·  5 HMC ·  256 runs · pool + native repro
Deep   · bytes_corpus · 10 HMC · 1000 runs · full corpus pass

Same economics you already know: 20% runs / 80% bounty escrow.
```

**4/8 — what customers actually get**
```
Deliverables that matter to a CTO:

• HTML security report (verdict + top issues)
• GET /api/fuzz/campaigns/{id}/gate — boolean pass for CI
• proof-bundle + escrow state on request
• read-only tracker on hackme.tech/fuzzing-console.html

Your keys stay on YOUR node. Report token ≠ admin token.
```

**5/8 — security (why we're not a random SaaS)**
```
Security model we refused to break for "convenience":

❌ No order creation on hackme.tech
❌ No from_code on the public origin
❌ No admin token in the browser

✅ Coordinator re-runs WASM on found nonces (workers can't fake gate pass)
✅ Pool payouts after server-side evalSubmitCheck
✅ Report access = X-Hackme-Report-Token only

Convenience lives on loopback. Attack surface stays thin.
```

**6/8 — CI**
```
GitHub Action pattern (copy from our repo):

curl gate?max_critical=0&max_high=0 \
  -H "X-Hackme-Report-Token: $SECRET"

pass=false → job fails. No findings leaked in logs.

examples/github-actions/hackme-fuzz-gate.yml
```

**7/8 — honest scope**
```
Honest scope:

• WASM check(i64)/check_bytes guards — not "we fuzzed your whole C++ repo"
• Distributed useful-PoW miners — not a rented SaaS container farm
• AGPL stack — verifiable, self-hostable

Perfect for: DeFi guards, script VMs, protocol invariants, CI gates before mainnet.
```

**8/8 — CTA**
```
Start:

1. hackme-node on your PC → 127.0.0.1:8080
2. hackme-fuzzing wizard --wasm guard.wasm --package audit
3. Watch pool · open report · wire gate into CI

Docs: hackme.tech/developers.html
Downloads: hackme.tech/downloads.html#fuzzing-client
Pool: hackme.tech/pool/coordinator

Questions? Managed audit pilots — DM open.
```

### Thread (RU) — 5 tweets

**1/5**
```
🛡️ B2B fuzz FINAL в HackMe.

guard.wasm → wizard → pool майнит → report + gate для CI.

Одна CLI-команда. Без админки в браузере на hackme.tech.

🧵
```

**2/5**
```
$ hackme-fuzzing wizard --wasm guard.wasm --package audit

Scan  1 HMC · 64 runs
Audit 5 HMC · 256 runs + pool
Deep 10 HMC · 1000 runs + corpus

Escrow 20/80 — как всегда.
```

**3/5**
```
Безопасность:

• заказы только на локальной ноде
• coordinator сам перепроверяет WASM (не верим worker)
• report token — только в header

Удобство — на loopback. Поверхность атаки — тонкая.
```

**4/5**
```
Для CI:

GET …/gate?max_high=0 + X-Hackme-Report-Token
pass=false → merge blocked

Шаблон Action в репозитории HackMe.
```

**5/5**
```
Старт: hackme.tech/downloads.html#fuzzing-client
Доки: hackme.tech/developers.html
Консоль: hackme.tech/fuzzing-console.html
```

---

## Telegram (@hackme_tech / @hackme_ru)

```
🛡️ B2B Fuzz FINAL — заказ аудита в одну команду

Раньше: 5 доков, curl, угадай budget.
Сейчас: hackme-fuzzing wizard → report + gate для CI.

━━━━━━━━━━━━━━━━━━━━
📦 Пакеты

Scan   ·  1 HMC ·   64 runs · wasm_only
Audit  ·  5 HMC ·  256 runs · pool + native
Deep   · 10 HMC · 1000 runs · byte corpus

Escrow 20/80 без изменений.

━━━━━━━━━━━━━━━━━━━━
⚡ Одна команда

hackme-fuzzing wizard \
  --wasm ./guard.wasm \
  --package audit \
  --title "My guard"

→ campaign_id + report token (один раз!)
→ report.html · pulse · gate?pass=true

Деньги — на вашей ноде (127.0.0.1).
Отчёт — на hackme.tech по report token.

━━━━━━━━━━━━━━━━━━━━
🔒 Безопасность

• нет заказов с публичного сайта
• coordinator re-verify WASM (fake gate pass не платим)
• wizard блокирует hackme.tech как base

━━━━━━━━━━━━━━━━━━━━
🔗 hackme.tech/developers.html
📥 downloads.html#fuzzing-client
📊 fuzzing-console.html (read-only)

Managed pilot — пишите в личку.
```

---

## Discord (#announcements)

**Title:** Product · B2B Fuzz Final — wizard, packages, CI gate

**Body:**
```
**TL;DR** — Security audits for WASM guards are now a product, not a research demo. One CLI command. Three packages. Report + CI gate.

**What's new**
• `hackme-fuzzing wizard` — Scan / Audit / Deep presets
• `fuzz_b2b_final_gate.sh` — one script, VERDICT.md for ops
• GitHub Action example for `GET …/gate`
• Coordinator server-side WASM re-verify on order submits

**Customer flow**
1. Local `hackme-node` → fund escrow
2. `wizard --wasm guard.wasm --package audit`
3. Pool runs distributed fuzz → HTML report + gate URL
4. Wire gate into CI (report token in secrets)

**Security (unchanged principles)**
• Orders on loopback only — not on hackme.tech
• Report token in header — not query string
• Pool payouts after server-side check re-exec

**Links**
• Developers: https://hackme.tech/developers.html
• CLI: https://hackme.tech/downloads.html#fuzzing-client
• Tracker: https://hackme.tech/fuzzing-console.html
```

---

## Reddit (r/netsec / r/crypto / profile — short)

**Title:** HackMe shipped B2B distributed fuzz — one CLI command, WASM guards, CI gate endpoint

**Body:**
We wrapped our useful-PoW fuzz stack into a customer-facing product:

- **hackme-fuzzing wizard** — upload a WASM `check()` guard, pick Scan (1 HMC) / Audit (5 HMC) / Deep (10 HMC)
- Miners on the public pool execute mutations; escrow splits 20% runs / 80% bounty
- Deliverables: HTML report + `GET /api/fuzz/campaigns/{id}/gate` for CI (report token auth)
- Security: orders stay on localhost; coordinator re-verifies WASM gates (workers can't fake passes)

Not OSS-Fuzz, not a black-box SaaS — AGPL, self-hostable, verifiable payouts.

Developers: https://hackme.tech/developers.html

---

## LinkedIn (optional long)

**Headline:** Useful-PoW meets B2B security audits — HackMe fuzz final

**Body:**  
We turned years of WASM-gated mining infrastructure into something protocol teams can actually buy: distributed fuzz campaigns with on-chain-style HMC escrow, verifiable reports, and a CI gate endpoint.

The `hackme-fuzzing wizard` maps your guard binary to one of three depth tiers, opens escrow on your local node, and registers work on the public pool. When the campaign completes, stakeholders get an HTML report and a machine-readable pass/fail gate — no admin keys in CI, no order forms on the public website.

If you're shipping script guards, VM invariants, or pre-mainnet WASM oracles, this is the path we use internally — now packaged.

https://hackme.tech/developers.html

---

## Hashtags (pick 2–3 max on X)

`#fuzzing` `#web3security` `#devtools` `#wasm` `#bugbounty` `#opensource`

Avoid hashtag spam on the main hook tweet.
