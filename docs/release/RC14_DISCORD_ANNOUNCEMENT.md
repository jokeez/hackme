## **HackMe `0.1.0-rc14` — pool fuzz customer-first (E2E proof)**

**TL;DR:** rc14 makes the pool spend cycles on **customer orders** first. Pool fuzz progress beats “local GPU time” for real orders.

**E2E before → after (Aug 11):**

| Metric | Before (broken/slow) | rc14 (after) |
|---|---:|---:|
| Claim latency p50 | ~6000 ms | ~1–5 ms |
| Pool throughput | ~4.6 done/min | ~25–32 done/min |
| Customer orders | 0/256 progress (starved) | **256/256** in ~26 min |
| Customer priority | ~23% share | **~4× faster** vs bootstrap-tier |

**Hybrid dig ON:** `2/50/2000/10%`  
Findings are generated on **customer campaigns** (not only bootstrap work).

**Infra fixes packaged for operators/miners:**
- `owner_ref` preserved
- bootstrap resync fixed (no more starving real orders)
- scheduling: FIFO wall + claim gap/backpressure tuned for customer flows

**Links**
- Downloads: https://hackme.tech/downloads.html
- GitHub release: https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14
- Pool: https://hackme.tech/pool/coordinator
- Fuzz guide: https://hackme.tech/fuzz-guide.html

Escrow remains **20/80** unchanged.

