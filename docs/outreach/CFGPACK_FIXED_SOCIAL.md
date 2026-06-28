# cfgpack upstream fix — social & news pack

**Case:** https://hackme.tech/reports/oss-cve/cases/cfgpack.html  
**Issue:** https://github.com/Arsievert/cfgpack/issues/1  
**Fix:** https://github.com/Arsievert/cfgpack/commit/b5e7cff  
**News:** https://hackme.tech/news.html#2026-06-28-cfgpack-upstream-fix

Publish:
```bash
FORCE_NEWS_ID=2026-06-28-cfgpack-upstream-fix NODE_SSH=hackme-vps \
  bash scripts/ops/publish_news_to_telegram.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_site.sh
```

---

## Twitter / X (@HackMeTech)

**Hero tweet (single):**
```
Not every security hunt ends with a CVE.

HackMe Tier-D fuzz → cfgpack MessagePack int64 decode → UBSan overflow.
Reported github.com/Arsievert/cfgpack/issues/1
Fixed upstream: b5e7cff ✓

No heap corruption. Real hardening. That's the pipeline working.

https://hackme.tech/reports/oss-cve/cases/cfgpack.html
```

**Thread (5 posts):**
1. We optimize for *upstream impact*, not CVE bingo. New case study: cfgpack — MessagePack int64 UBSan, reported, **fixed**.
2. How: mutation fuzz on stdin harness, clang + ASAN/UBSan. Malformed `0xd3` int64 bytes → `cfgpack_msgpack_decode_int64()` blows up on signed left-shift (msgpack.c:294).
3. We filed cfgpack#1 with minimal repro + suggested fix (assemble in uint64_t, then cast). Maintainer @Arsievert closed **completed** with b5e7cff.
4. Why not CVE? UBSan undefined behavior only — no ASAN memory corruption. Still worth fixing; still worth reporting. This is Tier-D doing its job *before* memory bugs ship.
5. Read the case · OSS hub · methodology → https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## Telegram

**@hackme_tech / @hackme_en**

🔬 **Upstream fix — cfgpack int64 UBSan**

HackMe Tier-D fuzz поймал overflow в `cfgpack_msgpack_decode_int64()` (MessagePack `0xd3`).

• Reported → [cfgpack#1](https://github.com/Arsievert/cfgpack/issues/1)  
• Fixed → [b5e7cff](https://github.com/Arsievert/cfgpack/commit/b5e7cff)  
• CVE: нет (UB only, без heap corruption)  
• Кейс → https://hackme.tech/reports/oss-cve/cases/cfgpack.html

Это не «нашли CVE ради CVE». Это нормальный responsible disclosure: нашли → починили upstream.

**@hackme_ru**

🔬 **cfgpack — UBSan починили upstream**

Наш fuzz (Tier-D) нашёл переполнение при разборе MessagePack int64. Отчёт [#1](https://github.com/Arsievert/cfgpack/issues/1) → мейнтейнер закрыл с фиксом [b5e7cff](https://github.com/Arsievert/cfgpack/commit/b5e7cff).

Не CVE — undefined behavior, без ASAN heap bug. Но это реальный вклад в open source.

Кейс: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## Discord (#research / #announcements)

**Title:** Research win · cfgpack MessagePack int64 — fixed upstream

**Body:**
```
:notepad_spiral: **TL;DR** — HackMe fuzz found UBSan in cfgpack's int64 decoder. Maintainer merged a fix. No CVE — still a W.

**Finding**
MessagePack tag `0xd3` + malformed payload → signed left-shift overflow in `cfgpack_msgpack_decode_int64()` (`msgpack.c:294`).

**Disclosure**
• Issue: https://github.com/Arsievert/cfgpack/issues/1
• Fix: https://github.com/Arsievert/cfgpack/commit/b5e7cff
• Class: UBSan / UB — not memory corruption

**Case study:** https://hackme.tech/reports/oss-cve/cases/cfgpack.html
**OSS hub:** https://hackme.tech/reports/oss-cve/
```

---

## Reddit (r/netsec / profile / r/oss)

**Title:** cfgpack MessagePack int64 UBSan — reported via fuzz, fixed upstream (not CVE)

**Body:**
We run Tier-D ASAN/UBSan mutation fuzz on small C parsers as part of the HackMe security research pipeline. On **Arsievert/cfgpack**, a malformed MessagePack int64 payload (`0xd3` + 8 bytes) triggered UBSan in `cfgpack_msgpack_decode_int64()` — signed left-shift overflow at `msgpack.c:294`.

We reported it as [cfgpack#1](https://github.com/Arsievert/cfgpack/issues/1). The maintainer closed it completed with fix [b5e7cff](https://github.com/Arsievert/cfgpack/commit/b5e7cff).

**Not CVE-class** — no ASAN heap corruption observed. Still a meaningful UB fix.

Case study (methodology, timeline): https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## LinkedIn (short)

HackMe security research: Tier-D fuzz → cfgpack MessagePack int64 UBSan → reported → upstream fix b5e7cff in 3 days. Not every finding is a CVE; responsible disclosure that lands patches is the metric we care about. Case study: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## Hacker News (Show HN style comment, if posting hub)

We published a case study on a small win from our OSS fuzz pipeline: cfgpack int64 decode UBSan, reported as GitHub issue #1, fixed in b5e7cff. No CVE — UBSan only. We think these outcomes are under-reported compared to CVE hype. Write-up: https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## BitcoinTalk / forum one-liner

[Research] HackMe Tier-D fuzz → cfgpack msgpack int64 UBSan → upstream fix b5e7cff (not CVE, real disclosure) — https://hackme.tech/reports/oss-cve/cases/cfgpack.html

---

## Suggested reply on GitHub issue #1

Thanks @Arsievert for the quick fix in b5e7cff — confirmed on our side. We've published a public case study (no weaponized PoC in the post): https://hackme.tech/reports/oss-cve/cases/cfgpack.html

Happy to help validate any follow-up int64/msgpack edge cases from our fuzz corpus.
