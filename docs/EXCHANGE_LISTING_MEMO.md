# HackMe Network — exchange listing memo (one page)

**Version:** 2026-08-07 · **Contact:** https://hackme.tech/contacts.html · **Repo:** https://github.com/jokeez/hackme (AGPL-3.0)

---

## What HackMe is

HackMe is **useful-compute infrastructure**: a native PoH chain (HMC), coordinator-backed mining pool, WASM security-audit orders, and a multi-coin ecosystem (SUP loyalty, HMS storage lane in prelaunch). Miners prove work through **hybrid-signed** submits; customers escrow HMC for fuzz/phasing campaigns.

**This is not** a classic Stratum SHA256 pool or an empty GPU coin.

## Primary listing asset: HMC

| Field | Value |
|-------|-------|
| Ticker | **HMC** |
| Name | HackMe Coin |
| Max supply | 100,000,000 |
| Decimals | 8 |
| Consensus | Proof of History + WASM gate |
| Block target | ~30s |
| Address format | `HMC-` + 16 hex chars |
| Signature | Ed25519 `transfer_v1` |
| Explorer | https://hackme.tech/explorer-lite.html |
| Node API | https://hackme.tech/api/status |
| Pool stats | https://hackme.tech/pool/coordinator/api/pool/stats |

**Genesis treasury (disclosed):** 50,000 HMC → `HMC-719006d93916ad52` (0.05% of max). Remaining supply = miner/order emission.

## Companion asset: SUP (after HMC lists)

| Field | Value |
|-------|-------|
| Ticker | **SUP** |
| Max supply | 21,000,000 |
| Earn | Quality-gated HMC pool mining only |
| On-chain | Live (`GET /api/sup/economics`) |
| Listing | Companion ticker post-HMC CEX |

## Differentiation vs typical PoW pools

| Typical pool | HackMe |
|--------------|--------|
| Stratum TCP shares | HTTP coordinator + hybrid Ed25519 |
| Blind hash grinding | WASM-gated useful segments |
| Single revenue = block subsidy | Block subsidy + **B2B audit escrow** |
| Opaque operator wallet | Public treasury + settlement timers |

## Technology transparency

- Open source node, coordinator, worker binaries
- Security research program: Bitcoin30 Week 1 + OSS CVE Watch (nghttp2 14/14 CLEAN · libheif 14/14 CLEAN)
- Policy regression locks in Go (`economics_test.go`)

## Integration pack

1. [EXCHANGE_LISTING_WALLET_PREP.md](EXCHANGE_LISTING_WALLET_PREP.md)
2. [spec/CHAIN_SPEC.md](../spec/CHAIN_SPEC.md)
3. [docs/API.md](API.md) — transfers section
4. Deposit test: `transfer_v1` + explorer confirmation

## Market / liquidity (honest)

- **Current stage:** early PoW network; **official pool live** (public stats API)
- **Near-term CEX targets:** Xeggex, NonKYC, TradeOgre (PoW-friendly first)
- **Liquidity:** no MM engaged yet — plan after first listing
- **No ROI promises** in official channels

## Legal

- Risk disclosures: https://hackme.tech/legal-risk.html
- Not registered as a security offering; utility + mining infrastructure narrative
- Entity / counsel: **in progress** for Tier-1 exchanges

## Social proof

- Bitcointalk ANN topic 5583373
- Telegram: @hackme_tech
- GitHub releases: `0.1.0-rc15`
- Pool stats: https://hackme.tech/pool/coordinator/api/pool/stats (live)
- Research: nghttp2 CVE Watch 14/14 CLEAN · libheif CVE Watch 14/14 CLEAN (~2.57B series exec · ASAN=0)

---

**Attachments (generate):** five branded PDFs — see [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md) and https://hackme.tech/listing.html
