# rc11q — social & community copy

Use after deploy confirms `https://hackme.tech/dist/release_0.1.0-rc11q/` is live.

---

## Telegram (@hackme_tech / @hackme_ru / @hackme_en)

**Headline:** rc11q — settlement unblocked + live banner

**Lead:** Win/Linux/ISO **0.1.0-rc11q** · hub payout API fixed · notices without rebuild

**Body bullets:**
- Fix: `/api/worker/settlement` — no more infinite load on hackme.tech hub
- New: Ecosystem banner pulls `miner-notices.json` (dismiss + download link)
- ISO finally matches Win/Linux tag — one **rc11q** everywhere
- Audit: `GET /api/status?lite=1` shows `sandbox_policy` + `admin_auth_enabled`

**Footer:** https://hackme.tech/downloads.html · pool: https://hackme.tech/pool/coordinator

**RU variant (коротко):**  
Вышел **HackMe 0.1.0-rc11q**. Панель settlement на хабе снова отвечает — начисление HMC в пуле не пропадало, просто UI зависал. Баннер обновления на вкладке Ecosystem тянется с сайта, переустанавливать exe ради текста не нужно. Win/Linux/ISO — один канал rc11q. Скачать: hackme.tech/downloads.html

---

## Twitter / X (@HackMeTech)

**Single tweet:**
```
HackMe 0.1.0-rc11q is live: hub settlement panel fixed, live upgrade banner from hackme.tech, Win/Linux/ISO on one tag.

Your pool accrual was fine — this is a client refresh.

https://hackme.tech/downloads.html
```

**Thread (optional):**
1. HackMe 0.1.0-rc11q — settlement on the public hub works again. We can also push upgrade notices without shipping a new installer just for copy.
2. Fix: `/api/worker/settlement` could time out while HMC kept accruing. rc11q uses cached coordinator stats + an 8s budget.
3. New: Ecosystem tab reads `miner-notices.json` — dismissible banner, download link, version check.
4. One channel: Windows installer, Linux tarball, HackMe OS ISO — all **0.1.0-rc11q**. Verify SHA256 before USB flash.
5. Pool: https://hackme.tech/pool/coordinator · Downloads: https://hackme.tech/downloads.html

---

## Discord (#announcements)

**Title:** Release 0.1.0-rc11q — settlement + live notices

**Body:**
```
**TL;DR** — Payout panel on the public hub responds again. One download channel (Win/Linux/ISO). Ecosystem tab can show upgrade notices from the website.

**Fixes**
• `/api/worker/settlement` — cached work stats, no hub timeout
• Lite status exposes sandbox_policy + admin_auth_enabled for audits

**New**
• Remote miner-notices.json → dismissible banner on Ecosystem
• ISO channel bumped to rc11q (was rc11o on CDN)

**Get it:** https://hackme.tech/downloads.html — check SHA256 before ISO flash.
```

---

## Reddit (r/gpumining / profile post — short)

**Title:** HackMe rc11q — settlement UI fix + aligned Win/Linux/ISO downloads

**Body:**  
We shipped **0.1.0-rc11q** for the HackMe HMC pool. If your desktop wallet settlement line spun forever while the coordinator still showed accrual, this build fixes that API path.  
Also: live upgrade banner on the Ecosystem tab (pulled from hackme.tech — no reinstall for announcement text).  
Downloads: https://hackme.tech/downloads.html · Live pool stats: https://hackme.tech/pool/coordinator  
SHA256 sums on the downloads page — verify before flashing HackMe OS ISO.

---

## Publish commands

```bash
FORCE_NEWS_ID=2026-06-27-rc11q-settlement-banner NODE_SSH=hackme-vps \
  bash scripts/ops/publish_news_to_telegram.sh
```
