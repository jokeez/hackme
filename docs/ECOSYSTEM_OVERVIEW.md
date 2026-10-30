# HackMe Network — ecosystem overview

**HackMe is a multi-asset useful-compute network**, not a single-coin mining pool. One operator stack (canonical node, coordinator, explorer) supports several tickers with separate ledgers and emission rules.

| Ticker | Name | Status | Role |
|--------|------|--------|------|
| **HMC** | HackMe Coin | **Live** | Primary chain reward, pool settlement, B2B order escrow |
| **SUP** | HackMe Support | **Live** (on-chain) | Quality-gated loyalty coin — companion to HMC |
| **HMS** | HackMe Storage | **Prelaunch** | Proof-of-Storage + seal lane (dedicated VPS) |
| **ORD** | Orders lane | Planned | Paid security-audit escrow mining |
| **SHD** | Shard pool | Planned | Horizontally scaled coordinators |

## Public surfaces (hackme.tech)

| Page | Purpose |
|------|---------|
| [coins.html](https://hackme.tech/coins.html) | Ecosystem registry + live pool strip |
| [exchange.hackme.tech](https://exchange.hackme.tech/) | Paper Exchange desk (D0) · also hub `#exchange` |
| [coin-hmc.html](https://hackme.tech/coin-hmc.html) | HMC listing package (investor-readable) |
| [coin-sup.html](https://hackme.tech/coin-sup.html) | SUP utility + emission |
| [coin-hms.html](https://hackme.tech/coin-hms.html) | HMS prelaunch preview |
| [token-transparency.html](https://hackme.tech/token-transparency.html) | Live supply / treasury / APIs |
| [roadmap.html](https://hackme.tech/roadmap.html) | Shipped milestones + next quarters |
| [listing.html](https://hackme.tech/listing.html) | CEX listing readiness hub (all tickers) — not the paper desk |
| [economics-model.html](https://hackme.tech/economics-model.html) | Three-layer HMC economics |

## Canonical docs (GitHub)

| Doc | Asset |
|-----|-------|
| [HMC_TOKENOMICS.md](HMC_TOKENOMICS.md) | Live supply, halving, fees, circulating |
| [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md) | Economics v2 soft announce (not height-activated) |
| [SUP_TOKENOMICS.md](SUP_TOKENOMICS.md) | Pool accrual, caps, on-chain mint |
| [HMS_TOKENOMICS.md](HMS_TOKENOMICS.md) | Storage lane epoch budget |
| [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) | Allocation table + unlock calendar |
| [EXCHANGE_LISTING_MEMO.md](internal/EXCHANGE_LISTING_MEMO.md) | One-page CEX submission memo |
| [LISTING_PITCH_OUTLINE.md](internal/LISTING_PITCH_OUTLINE.md) | 10–12 slide institutional outline |
| [DOCUMENTATION_EXPORT.md](internal/DOCUMENTATION_EXPORT.md) | PDF/DOCX export per ticker |
| [EXCHANGE_LISTING_WALLET_PREP.md](internal/EXCHANGE_LISTING_WALLET_PREP.md) | Technical integration pack |

## Differentiation (one paragraph)

HackMe combines **Blake3 PoH share-PPS mining** (honest wall/share credit, not “useful PoW” by itself), a **coordinator-backed fair pool** (hybrid PoH + Dig/Hunt fuzz, Ed25519 submit — not blind Stratum shares), and a **B2B security-audit layer** (`hackme-fuzzing wizard` Dig packs + **Hunt** ASAN via published `harness_hash` — customer git stays on the local node; pool miners run the harness binary, escrow + `fuzz_report_v2` / Hunt reports). **Useful customer work lives on Dig/Hunt fuzz lanes**; Blake3 PoH pays verified shares. HMC is the settlement rail; SUP rewards long-horizon honest miners; HMS extends the same stack to storage economics.

## Listing readiness (honest)

| Dimension | Today | Target |
|-----------|-------|--------|
| Technology / transparency | Strong public APIs, docs, explorer | Maintain + per-ticker PDF packs |
| Operational discipline | Settlement timers, public APIs, release channel docs | Scale soak + HA narrative |
| Market / traction | Pool live · Hunt Watch 12/12 · OSS CVE Watch · own spot desk | Desk volume + organic miners/customers |
| Liquidity | Own paper desk only · no external MM | MM after desk traction / later CEX |
| Legal | Risk disclosures on site | Entity + counsel before Tier-1 |

**Strategy:** deepen **own spot desk** (no futures at start) + miners/customers → then **HMC** on PoW-friendly external CEX when MM exists → **SUP** companion → **HMS** when lane is live. Binance-tier is a later stage, not the near-term promise. Mid-tier paid CEX cold outreach is deferred.
