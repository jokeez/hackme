# cfgpack upstream fix — social & news pack (v2)

**Case study:** https://hackme.tech/reports/oss-cve/cases/cfgpack.html  
**GitHub issue:** https://github.com/Arsievert/cfgpack/issues/1  
**Fix commit:** https://github.com/Arsievert/cfgpack/commit/b5e7cff  
**News:** https://hackme.tech/news.html#2026-06-28-cfgpack-upstream-fix  
**OSS hub:** https://hackme.tech/reports/oss-cve/

**One-line pitch:** We fuzzed a tiny MessagePack parser, caught undefined behavior before it became memory corruption, reported it, and the maintainer shipped a fix in days — no CVE theatre required.

Publish:
```bash
FORCE_NEWS_ID=2026-06-28-cfgpack-upstream-fix NODE_SSH=hackme-vps \
  bash scripts/ops/publish_news_to_telegram.sh
NODE_SSH=hackme-vps SKIP_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

---

## Twitter / X (@HackMeTech)

### Hero tweet (single — best for quote-retweet)

```
The best security outcomes don't always have a CVE number.

Our Tier-D fuzz hit cfgpack's MessagePack int64 decoder with one bad byte sequence.
UBSan screamed. We reported it. Maintainer merged a fix.

Report → github.com/Arsievert/cfgpack/issues/1
Fix   → b5e7cff

No heap corruption. Still a real win.

Case study ↓
https://hackme.tech/reports/oss-cve/cases/cfgpack.html
```

### Alt single (shorter, punchier)

```
CVE bingo isn't the only scoreboard.

cfgpack int64 decode · UBSan overflow · reported #1 · fixed upstream ✓

https://hackme.tech/reports/oss-cve/cases/cfgpack.html
```

### Thread (7 posts — narrative arc)

**1/7**  
Security Twitter loves CVE drops. We love *merged fixes*.  
New write-up: how HackMe Tier-D fuzz turned a malformed MessagePack int64 into an upstream patch — in days, not drama. 🧵

**2/7**  
Target: **Arsievert/cfgpack** — small C library, MessagePack decode.  
Method: mutation fuzz on a stdin harness, clang 18, ASAN + UBSan.  
Same pipeline we use for Bitcoin guard research — applied to OSS parsers.

**3/7**  
Trigger: MessagePack tag `0xd3` (int64) + 8 hostile bytes.  
UBSan: signed left-shift overflow in `cfgpack_msgpack_decode_int64()` — `msgpack.c:294`.  
Classic C UB: shift a signed value past what `int64_t` can represent.

**4/7**  
We filed **cfgpack#1** with minimal repro + a concrete fix idea (assemble in `uint64_t`, cast to `int64_t`).  
Maintainer @Arsievert closed it **completed** with **b5e7cff**.  
That's the outcome we optimize for.

**5/7**  
Why no CVE?  
ASAN showed **no heap corruption** — UBSan undefined behavior only.  
Honest classification > chasing a number. UB today often becomes memory bugs tomorrow if ignored.

**6/7**  
This is what Tier-D hunts are for:  
→ catch UB early  
→ report responsibly  
→ publish case status publicly  
→ withhold weaponized PoC until appropriate  

**7/7**  
Full timeline + links:  
https://hackme.tech/reports/oss-cve/cases/cfgpack.html  
More hunts: https://hackme.tech/reports/oss-cve/  
Methodology: github.com/jokeez/hackme (AGPL-3.0)

`#infosec` `#fuzzing` `#opensource` `#responsibledisclosure`

---

## Telegram

### @hackme_tech · channel (RU, pin-friendly)

```
🔬 Research · upstream fix (не CVE — но реальная победа)

HackMe Tier-D fuzz нашёл UBSan в cfgpack — MessagePack int64 decoder.

Что случилось:
• Байты 0xd3 + malformed payload → overflow в cfgpack_msgpack_decode_int64()
• Файл msgpack.c:294 — signed left-shift UB
• Отчёт → github.com/Arsievert/cfgpack/issues/1
• Мейнтейнер закрыл completed → fix b5e7cff

Почему без CVE:
ASAN heap corruption не было. Это hardening UB — ровно то, ради чего мы гоняем Tier-D.

📎 Кейс: hackme.tech/reports/oss-cve/cases/cfgpack.html
📂 Все кейсы: hackme.tech/reports/oss-cve/

Мы не продаём «нашли CVE». Мы строим pipeline: fuzz → report → patch upstream.

🐦 @HackMeTech · код AGPL: github.com/jokeez/hackme
```

### @hackme_en (EN)

```
🔬 Research win · cfgpack fixed upstream

HackMe Tier-D fuzz → MessagePack int64 decode → UBSan overflow → maintainer patch.

• Finding: malformed 0xd3 int64 path, msgpack.c:294
• Reported: github.com/Arsievert/cfgpack/issues/1
• Fixed: b5e7cff (issue closed completed)
• CVE: none — UB only, no ASAN heap bug

Not every hunt ends in a CVE. Some end better: a merged fix.

📎 Case study: hackme.tech/reports/oss-cve/cases/cfgpack.html
📂 OSS hub: hackme.tech/reports/oss-cve/

Source AGPL-3.0 · github.com/jokeez/hackme
```

### @hackme_ru (короткий, для чата)

```
Коротко про research:

Наш fuzz поймал баг в cfgpack (MessagePack int64) — UBSan, не heap overflow.
Отчёт #1 → мейнтейнер починил (b5e7cff).

Не CVE, но нормальный responsible disclosure: нашли → upstream fix.

Кейс: hackme.tech/reports/oss-cve/cases/cfgpack.html
```

---

## Reddit

### r/netsec (primary)

**Title:** `[Research] cfgpack MessagePack int64 UBSan — fuzz-found, upstream fixed in b5e7cff (not CVE-class)`

**Body:**

We run a Tier-D mutation fuzz pipeline (clang ASAN/UBSan, stdin harnesses) on small C parsers as part of HackMe's OSS security research. Disclosure policy and case cards are public: https://hackme.tech/reports/oss-cve/

**Finding**

Library: [Arsievert/cfgpack](https://github.com/Arsievert/cfgpack)  
Function: `cfgpack_msgpack_decode_int64()`  
Trigger: MessagePack int64 tag `0xd3` followed by 8 arbitrary bytes  
Sanitizer: UBSan — signed left-shift overflow at `msgpack.c:294`  
ASAN: no heap corruption observed

**Disclosure**

- Reported: https://github.com/Arsievert/cfgpack/issues/1  
- Maintainer closed **completed** with fix https://github.com/Arsievert/cfgpack/commit/b5e7cff  
- Timeline: reported 2026-06-25 → fix merged 2026-06-28

**Why we're posting**

Most write-ups only celebrate CVE assignments. We think merged upstream fixes on UB findings deserve visibility too — especially in parsers that end up in config/tooling stacks.

Full case study (timeline, classification, links):  
https://hackme.tech/reports/oss-cve/cases/cfgpack.html

Happy to answer methodology questions. We're the HackMe operator; repo is AGPL-3.0 on GitHub.

---

### r/opensource or r/programming (lighter tone)

**Title:** `Small C parser, one bad MessagePack byte sequence, and a maintainer who actually fixed it`

**Body:**

Our security research pipeline fuzzed **cfgpack** (MessagePack decoder). One mutation produced a `0xd3` int64 payload that made UBSan unhappy — signed shift overflow in the decode loop.

We reported it. Maintainer shipped a fix. No CVE, no hype cycle — just a clean closed loop in a few days.

Write-up: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

If you maintain a small C parser: sanitizer fuzz is cheap insurance.

---

### Profile / r/crypto (one-liner for cross-post)

Research note: HackMe Tier-D fuzz → cfgpack int64 UBSan → upstream fix b5e7cff. Not CVE-class (UB only). Case: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## BitcoinTalk

**Thread title:** `[Research] HackMe fuzz → cfgpack MessagePack int64 UBSan → upstream fix (no CVE — real disclosure)`

**Post body (copy-paste ready):**

```
Hello Bitcointalk,

Short research update from the HackMe security pipeline — not mining news, but 
relevant to anyone who cares about parser hardening in tooling stacks.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 WHAT HAPPENED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

We run Tier-D mutation fuzz (clang + ASAN/UBSan) on small open-source C parsers.
One target: Arsievert/cfgpack — a compact MessagePack decoder.

A malformed int64 payload (MessagePack tag 0xd3 + 8 bytes) triggered UBSan:

  → function: cfgpack_msgpack_decode_int64()
  → location: msgpack.c:294
  → class:   signed integer left-shift overflow (undefined behavior)

AddressSanitizer did NOT report heap corruption on this input.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 DISCLOSURE OUTCOME
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Reported:  https://github.com/Arsievert/cfgpack/issues/1
  Fixed:     https://github.com/Arsievert/cfgpack/commit/b5e7cff
  Status:    maintainer closed issue as COMPLETED
  CVE:       none (UBSan / UB — not memory corruption)

Timeline: reported 25 Jun 2026 → fix merged 28 Jun 2026.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 WHY POST THIS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

CVE headlines get the clicks. Merged fixes on UB findings rarely get mentioned.
We publish case cards for both — because catching UB *before* it becomes a 
memory bug is how you keep infrastructure boring (in a good way).

Full case study (timeline, links, honest scope):
  → https://hackme.tech/reports/oss-cve/cases/cfgpack.html

More OSS hunt cases:
  → https://hackme.tech/reports/oss-cve/

Methodology + source (AGPL-3.0):
  → https://github.com/jokeez/hackme

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 HACKME CONTEXT (for newcomers)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

HackMe is an open GPU mining network (HMC) with a public security research arm:
Bitcoin guard fuzzing, OSS parser hunts, coordinated disclosure.

  Site:      https://hackme.tech
  Pool:      https://hackme.tech/pool/coordinator
  Downloads: https://hackme.tech/downloads.html

No ROI talk here — this post is research only.

Questions welcome. We'll answer what we can without publishing weaponized PoC.

— HackMe research / jokeez
```

**Signature block (optional):**

```
---
HackMe Network · useful PoW · open source security research
https://hackme.tech · AGPL-3.0 · https://github.com/jokeez/hackme
```

---

## Discord (#research / #announcements)

**Title:** 🔬 cfgpack — MessagePack int64 UBSan fixed upstream

```
**TL;DR:** Fuzz found UB in a MessagePack decoder. Maintainer merged a fix. No CVE — still a W.

━━━━━━━━━━━━━━━━━━━━━━
🔍 The finding
━━━━━━━━━━━━━━━━━━━━━━
**Library:** Arsievert/cfgpack
**Path:** MessagePack `0xd3` int64 decode
**Bug:** signed left-shift overflow · `msgpack.c:294`
**Sanitizer:** UBSan ✓ · ASAN heap corruption ✗

━━━━━━━━━━━━━━━━━━━━━━
✅ The outcome
━━━━━━━━━━━━━━━━━━━━━━
• Issue: https://github.com/Arsievert/cfgpack/issues/1
• Fix: https://github.com/Arsievert/cfgpack/commit/b5e7cff
• Closed: **completed** by maintainer

━━━━━━━━━━━━━━━━━━━━━━
📖 Read more
━━━━━━━━━━━━━━━━━━━━━━
Case study → https://hackme.tech/reports/oss-cve/cases/cfgpack.html
OSS hub   → https://hackme.tech/reports/oss-cve/

*We publish status early; full PoC only when disclosure window allows.*
```

---

## LinkedIn

HackMe security research doesn't optimize for CVE bingo.

We fuzz small C parsers with Tier-D ASAN/UBSan mutation. On cfgpack (MessagePack), a malformed int64 payload exposed signed shift undefined behavior in `cfgpack_msgpack_decode_int64()`.

We reported it. The maintainer fixed it in days (b5e7cff). No heap corruption — honest classification, real upstream impact.

Case study: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

#cybersecurity #opensource #fuzzing #responsibledisclosure

---

## Hacker News (comment or Show HN companion)

We published a case study on a disclosure outcome that doesn't get much airtime: UBSan finding → GitHub issue → merged fix, no CVE. cfgpack MessagePack int64 decoder, fuzz-found, fixed in b5e7cff. Honest write-up: https://hackme.tech/reports/oss-cve/cases/cfgpack.html — curious if others think UB-only fixes deserve more visibility than CVE drops.

---

## GitHub reply on issue #1

Thanks @Arsievert for the fast turnaround on **b5e7cff** — we confirmed the fix addresses the UBSan path we reported.

We've published a public case study (timeline + classification, no weaponized PoC in the post):  
https://hackme.tech/reports/oss-cve/cases/cfgpack.html

Happy to share additional fuzz corpus seeds privately if useful for regression tests on int64/msgpack edge cases.

---

## Quick reference card (all platforms)

| Field | Value |
|-------|-------|
| Library | Arsievert/cfgpack |
| Component | MessagePack int64 (`0xd3`) |
| Function | `cfgpack_msgpack_decode_int64()` |
| Line | msgpack.c:294 |
| Class | UBSan — signed shift overflow |
| CVE | **none** (no ASAN memory corruption) |
| Issue | [#1](https://github.com/Arsievert/cfgpack/issues/1) |
| Fix | [b5e7cff](https://github.com/Arsievert/cfgpack/commit/b5e7cff) |
| Case | [hackme.tech/.../cfgpack.html](https://hackme.tech/reports/oss-cve/cases/cfgpack.html) |
