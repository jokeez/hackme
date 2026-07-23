# Template: first influx of miners after MiningPoolStats

Copy to **Telegram (pinned)** / **Discord #mining** / reply to Bitcointalk. Substitute the actual numbers from https://hackme.tech/pool/coordinator/api/pool/stats.

---

**HackMe Official Pool — live on MiningPoolStats**

- **Site:** https://hackme.tech  
- **Explorer:** https://hackme.tech/pool/explorer  
- **Pool stats:** https://hackme.tech/pool/coordinator/api/pool/stats  
- **Downloads:** https://hackme.tech/downloads.html  
- **Source:** https://github.com/jokeez/hackme  

**How to mine (not Stratum)**  
1. Download `hackme-node` + `workerpoh` (+ `minersign`) for your OS.  
2. Point worker at coordinator: `https://hackme.tech/pool/coordinator`  
3. Set your **payout address** (`HMC-…`) via hybrid signer / `WORKER_PAYOUT_MAP` on the pool host.  
4. Pool pays **accepted work** (reward/M per attempt) + rare **found bonus**; settlement to chain runs automatically on the pool VPS (typically within ~30s above min payout).

**Economics (check live, do not trust static numbers)**  
- Pool GH/s, workers, reward/M: dashboard on site or `GET /api/global/metrics`  
- Calculator: built into the desktop / node dashboard (syncs with coordinator)

**Rules**  
- No inflated submits — hybrid Ed25519 signature required.  
- Fair load retarget on pool size (target_mod).  
- Questions: support@hackme.tech · ANN: https://bitcointalk.org/index.php?topic=5583373.0  

*Replace ROI claims with your own power cost; pool is early — verify payouts on explorer before scaling rigs.*

---

## English short (Twitter / X)

HackMe $HMC pool is listed on MiningPoolStats. Useful-PoW / WASM + HTTP coordinator (no Stratum). Mine with workerpoh → https://hackme.tech — explorer & open source on GitHub. Early network; DYOR.
