# Institutional pitch outline (10–12 slides)

Markdown source for PDF/Google Slides export. One deck for the **network**; append per-coin annexes (HMC, SUP, HMS).

---

## Slide 1 — Title

**HackMe Network** — Blake3 PoH share-PPS + Dig/Hunt useful-compute pool  
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
3. **Orders / fuzz** — Dig (WASM) + Hunt (ASAN) escrow campaigns on the same network
4. **Own spot desk + SUP wallet** — exchange.hackme.tech paper soft (no futures at start; not external CEX custody)

## Slide 4 — Ecosystem map

| Ticker | Role | Status |
|--------|------|--------|
| HMC | Settlement + mining | Live |
| SUP | Loyalty / utility | Live on-chain |
| HMS | Storage + seal | Prelaunch |

## Slide 5 — Technology proof

- Live pool + explorer + downloads (**0.1.0-rc18** LIVE · installers published)
- Hunt Watch 2026sep: **12/12 CLOSED** (~192.5M Hunt Standard · ASAN+UBSan) · [ledger](https://hackme.tech/reports/hunt-watch-2026sep/)
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

- **Now:** own spot desk (exchange.hackme.tech) · deepen miners + Dig/Hunt customers · GHS↔Dig coupling live
- Research ledgers closed (Hunt Watch 12/12 · Bitcoin30 · nghttp2 14/14 · libheif 14/14)
- **Later:** external PoW-friendly CEX when MM + traction · aggregators · legal entity for Tier-1
- Mid-tier paid CEX cold outreach: deferred while desk/product are priority

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
