# Institutional pitch outline (10–12 slides)

Markdown source for PDF/Google Slides export. One deck for the **network**; append per-coin annexes (HMC, SUP, HMS).

---

## Slide 1 — Title

**HackMe Network** — Useful Proof-of-History infrastructure  
HMC · SUP · HMS ecosystem · hackme.tech  
*Not investment advice*

## Slide 2 — Problem

- GPU mining often produces **no verifiable output**
- Security teams need **repeatable fuzz** with economic accountability
- PoW projects lack **transparent operator discipline**

## Slide 3 — Solution

Three layers on one stack:

1. **Chain (HMC)** — PoH blocks, transfers, emission
2. **Coordinator pool** — fair attempt accounting, hybrid signatures
3. **Orders / fuzz** — B2B escrow for WASM security campaigns

## Slide 4 — Ecosystem map

| Ticker | Role | Status |
|--------|------|--------|
| HMC | Settlement + mining | Live |
| SUP | Loyalty / utility | Live on-chain |
| HMS | Storage + seal | Prelaunch |

## Slide 5 — Technology proof

- Live pool + explorer + downloads (**0.1.0-rc13**)
- OSS CVE Watch: nghttp2 14/14 CLEAN · libheif 14/14 CLEAN (~2.57B series exec · ASAN=0)
- Operator gates: miner launch, SUP verdict, site consistency
- Private bounty B1–B5 patched (desktop rebind, fleet settle, CF IP, fuzz PoP)
- AGPL source on GitHub

## Slide 6 — HMC tokenomics (summary)

- 100M max · halving + tail · 50k genesis treasury (0.05%)
- Circulating = API-verifiable
- Utility: mining, fees, order escrow

## Slide 7 — SUP & HMS (companion assets)

- **SUP:** 21M cap, pool-only, anti-farm gates, CEX after HMC
- **HMS:** 21M cap, storage economics, Amsterdam heavy VPS

## Slide 8 — Business model

- Miners: HMC + SUP from honest pool work
- Customers: prepaid HMC for security audits / fuzz
- Long-term: storage lane (HMS) B2B backup revenue

## Slide 9 — Traction (honest)

- Public pool on hackme.tech
- Official pool: https://hackme.tech/pool/coordinator/api/pool/stats
- Research reports + Telegram channel
- **No CEX volume yet**

## Slide 10 — Roadmap

- Q3 2026: own exchange paper soft (~15 Sep) · order clients · B2B integrators
- Research ledgers closed (Bitcoin30 W1 · nghttp2 14/14 · libheif 14/14) — architecture + pool narrative next
- Later: foreign PoW CEX if gates · aggregators, MM, legal entity for Tier-1

## Slide 11 — Risks

- Single canonical hub (decentralization narrative evolving)
- Early-stage liquidity
- Regulatory classification varies by jurisdiction
- HMS not live — do not market HMS as tradable

## Slide 12 — Ask / contacts

- **Listing:** integration pack + deposit test wallet
- **B2B:** security audit pilots via local node
- https://hackme.tech/contacts.html

---

## Annex A — HMC technical (optional slides)

- CHAIN_SPEC excerpt, API endpoints, wallet integration steps

## Annex B — SUP companion listing

- SUP_TOKENOMICS.md summary, economics API screenshot

## Annex C — HMS prelaunch

- HMS_PUBLIC_ROADMAP.md timeline only

Export: [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md)
