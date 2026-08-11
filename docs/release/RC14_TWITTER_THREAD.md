**HackMe `0.1.0-rc14` — pool fuzz gets customer-first priority** 🧵

**Post 1/10**

HackMe `0.1.0-rc14` is live.

rc14 focuses on one operational truth: **pool fuzz by order beats “local GPU time”** for real customers.

Downloads: https://hackme.tech/downloads.html  
GitHub release: https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14

**Post 2/10**

Before → After (E2E evidence, Aug 11):

| Metric | Before (broken/slow) | rc14 (after) |
|---|---:|---:|
| Claim latency p50 | ~6000 ms | ~1–5 ms |
| Claim latency p95 | ~12–20 s | (kept low under customer-first scheduling) |
| Customer priority | ~mixed | **customer-first** |

**Post 3/10**

Pool throughput impact (same test family):

| Metric | Before | rc14 |
|---|---:|---:|
| Pool “done” rate | ~4.6 done/min | ~25–32 done/min |
| Customer orders | 256 / 256 | **256 / 256** |
| Customer completion time | (starved in old path) | ~26 min (~9.8/min) |

**Post 4/10**

Customer claim share:

- Before: **~23%**
- rc14: **~4× faster than bootstrap-tier** in parallel A/B

Translation: the pool is spending more cycles on **customer orders**, not on “the wrong tier first”.

**Post 5/10**

“Will pool fuzz be faster than a miner’s GPU?”

In the same order context:

- **rc14 pool:** 3 pool workers
- **vs 1 local GPU:** RTX 5060 Ti-class
- Result: **pool beats local GPU** (order-level progress wins).

**Post 6/10**

Hybrid dig is now ON (and actually useful on customer campaigns):

- concurrency: `2/50/2000/10%`
- hybrid dig idle fixed: no more concurrency=1 lock
- FINDINGs on customer campaigns (not only bootstrap work)

**Post 7/10**

Operational / infra fixes included in rc14 packages:

- `owner_ref` preserved (no “identity drift”)
- bootstrap resync fixed (no more starving real orders)
- Pool scheduling: FIFO wall reduced; claim gap + backpressure tuned for customer flows

**Post 8/10**

Product message (keep it simple):

**Pool fuzz by order beats local GPU.**

Escrow **20/80** unchanged.

rc14 primarily ships infra fixes for miners/operators running the pool + fleet.

**Post 9/10**

What to try:

1. Pull the release: https://hackme.tech/downloads.html
2. Watch the pool coordinator: https://hackme.tech/pool/coordinator
3. Use the fuzz guide (for B2B/B2C integration patterns): https://hackme.tech/fuzz-guide.html

**Post 10/10**

Scope note (honest, concise):

These numbers reflect **pool + fuzz scheduling behavior under real E2E order flows**.
Not “guaranteed uptime”, not a trustless L1 promise, and not exchange-readiness.

#HackMe #UsefulPoW #Mining #Fuzzing

