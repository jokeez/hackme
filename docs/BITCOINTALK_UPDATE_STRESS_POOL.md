# Bitcointalk — update post (reply in existing ANN thread)

**Thread:** https://bitcointalk.org/index.php?topic=5583373.0  
**Paste:** [BITCOINTALK_UPDATE_BBCode.txt](BITCOINTALK_UPDATE_BBCode.txt)  
**When:** After verifying live stats on https://hackme.tech/pool/coordinator/api/pool/stats

---

## Suggested reply title

`Re: [ANN] HackMe — coordinator stress test · MPS listing · rc11i Windows OpenCL`

---

## English (plain text preview)

**Update — May 2026**

We completed a **10-minute coordinator mega stress test** on an isolated stress profile (100 virtual workers, target ~25 req/s each, claim/submit flood, chaos disconnects, 1000 malformed payloads):

| Result | |
|--------|---|
| Hard errors (5xx, timeouts) | **0%** |
| Coordinator crash | **none** |
| RAM (10 min) | ~12 → ~20 MB, **no leak** (flat slope after warmup) |
| Latency p50 / p99 | **0.9 ms / 10.3 ms** |
| Halving block **2,102,401** | reward **0.01 → 0.005 HMC** verified under load |
| Malformed ingress | **1000/1000** fast-rejected (400), no slow-parse hangs |

Under extreme RPS the pool applies **429 backpressure** (rate limits) instead of corrupting state — by design.

**Public pool (production):** https://hackme.tech  
- **Not Stratum** — `hackme-node` + `workerpoh` talk HTTP to `/pool/coordinator`  
- Live stats: https://hackme.tech/pool/coordinator/api/pool/stats  
- Explorer / payouts: https://hackme.tech/pool/explorer  
- Downloads (SHA256 on page): https://hackme.tech/downloads.html  

**Miners**
- **Windows:** `HackMe-Setup-0.1.0-rc11i.exe` — installer, pool token preconfigured, **OpenCL** for AMD (RX 580 profile).  
- **Linux:** CUDA (NVIDIA) or OpenCL or CPU — see repo `docs/GPU_MINING_BACKENDS.md`.  
- Set **`WORKER_PAYOUT_MAP`** / hybrid signer so your worker id → **`HMC-…`** address for settlement.

**Listing**
- **MiningPoolStats** submission in progress (HTTP coordinator — we note *not Stratum* in pool description).  
- **ANN:** this thread · **Source:** https://github.com/jokeez/hackme (Apache-2.0)

**Trust**
- Official site only: **https://hackme.tech**  
- Security reports: https://hackme.tech/contacts.html (no public 0-days)

Questions welcome in-thread. We post shorter updates on Telegram when releases ship.

*Experimental RC software — DYOR, verify payouts on explorer before scaling hash.*

---

## Русский (для себя / Telegram, не обязательно в BCT)

См. [TELEGRAM_POST_STRESS_POOL.md](TELEGRAM_POST_STRESS_POOL.md)
