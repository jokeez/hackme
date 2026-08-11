[b]Re: HackMe Network — Useful PoW + Pool Fuzz[/b]

[b]Update: 0.1.0-rc14[/b] (2026-08-11) — customer-first pool fuzz scheduling + infra fixes for miners

[b]TL;DR[/b]

In E2E order flows on Aug 11, rc14 reduced claim latency from seconds to milliseconds and increased pool completion throughput ~5–7×.
The pool now prioritizes **customer orders** (not bootstrap-only starvation), with hybrid dig on customer campaigns.

[b]Before → After (E2E evidence)[/b]

[code]
Claim latency p50:   ~6000 ms  →  ~1–5 ms
Claim latency p95:   ~12–20 s  →  (stays low under customer-first priority)

Pool throughput:     ~4.6 done/min → ~25–32 done/min
Customer orders:     256/256 → 256/256 completed in ~26 min (~9.8/min)

Customer-first share:
  ~23% (before) → ~4× faster than bootstrap-tier in parallel A/B

Hybrid dig:
  idle path (concurrency=1, claim_gap 400ms, backpressure 35%) → fixed
  hybrid dig ON: 2/50/2000/10%, FINDINGs on customer campaigns
[/code]

[b]What rc14 actually ships[/b]

[list]
[*] **Customer-first priority:** scheduling tuned so pool fuzz progress tracks real customer order completions.
[*] **Hybrid dig is ON and effective:** FINDINGs happen on customer campaigns.
[*] **Identity + resync correctness:** `owner_ref` preserved; bootstrap resync fixed.
[*] **Escrow economics unchanged:** escrow remains [b]20/80[/b].
[/list]

[b]For miners / operators[/b]

rc14 is designed for running the coordinator + fleet without the earlier slow/broken paths:

[list]
[*] pool workers: rc14 scales by order throughput (not “one GPU at a time”)
[*] operational fixes: less scheduling wall time; tuned claim gap/backpressure
[*] bootstrap starvation: removed via resync + customer-first priorities
[/list]

Pool coordinator:
https://hackme.tech/pool/coordinator

[b]For B2B fuzz teams (buying fuzz via orders)[/b]

If you’re integrating fuzz through customer orders, rc14 is the release you want because:

[list]
[*] orders complete faster in the same test envelope (256/256 in ~26 min)
[*] pool cycles are spent where customers actually need progress
[*] hybrid dig/fuzz runs together on the same customer workload
[/list]

Fuzz guide:
https://hackme.tech/fuzz-guide.html

[b]Downloads / GitHub release[/b]

Downloads:
https://hackme.tech/downloads.html

GitHub release:
https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14

[b]Scope note (honest)[/b]

This post reports E2E pool + scheduling behavior under real order flows.
It is not a claim of guaranteed future uptime, not a trustless L1 promise, and not exchange readiness.

[b]Thanks[/b]

Pool ops, fuzz buyers, and miner operators make the measurements possible. Verify downloads before running.

