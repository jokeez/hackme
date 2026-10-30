# HackMe Network — exchange listing memo (one page)

**Version:** 2026-10-16 · **Contact:** https://hackme.tech/contacts.html · **Repo:** https://github.com/jokeez/hackme (AGPL-3.0)

---

## What HackMe is

HackMe is **useful-compute infrastructure**: a native PoH chain (HMC), coordinator-backed mining pool, Dig/Hunt security-audit escrow, and a multi-coin ecosystem (SUP loyalty, HMS storage lane in prelaunch). Miners prove work through **hybrid-signed** submits; customers escrow HMC for fuzz campaigns. Live hybrid GPUs soft-prioritize Dig/Hunt leases (GHS↔Dig coupling); ASAN/WASM evaluation stays on **CPU**.

**This is not** a classic Stratum SHA256 pool or an empty GPU coin.

## Listing sequence (honest)

1. **Own spot desk first** — https://exchange.hackme.tech/ (paper soft / D0; no futures lane at start) so miners and audit customers can acquire HMC without mid-CEX dump dynamics  
2. **Grow miners + B2B Dig/Hunt demand**  
3. **External PoW-friendly CEX later** (when MM + traction exist)  
4. **SUP companion**, then **HMS** when storage is live  
5. **Tier-1** only after legal entity + real volume  

Cold mid-tier CEX pitches are **deferred** while the desk and product deepen.

## Primary listing asset: HMC

| Field | Value |
|-------|-------|
| Ticker | **HMC** |
| Name | HackMe Coin |
| Max supply | 100,000,000 |
| Decimals | 8 |
| Consensus | Blake3 Proof of History (fv≥3); WASM on Dig/Hunt orders |
| Block target | ~30s |
| Address format | `HMC-` + 16 hex chars |
| Signature | Ed25519 `transfer_v1` |
| Explorer | https://hackme.tech/explorer-lite.html |
| Node API | https://hackme.tech/api/status |
| Pool PoH stats | https://hackme.tech/pool/coordinator/api/work/stats |
| Dig/Hunt capacity | https://hackme.tech/pool/coordinator/api/fuzz/pool/stats |

**Genesis treasury (disclosed):** 50,000 HMC → `HMC-719006d93916ad52` (0.05% of max). Remaining supply = miner/order emission.

## Companion asset: SUP (after HMC has spot traction)

| Field | Value |
|-------|-------|
| Ticker | **SUP** |
| Max supply | 21,000,000 |
| Earn | Quality-gated HMC pool mining only |
| On-chain | Live (`GET /api/sup/economics`) |
| Listing | Companion ticker after HMC desk/CEX traction |

## Differentiation vs typical PoW pools

| Typical pool | HackMe |
|--------------|--------|
| Stratum TCP shares | HTTP coordinator + hybrid Ed25519 |
| Blind hash grinding | Dig/Hunt fuzz escrow (useful work); Blake3 = share-PPS |
| Single revenue = block subsidy | Block subsidy + **B2B audit escrow** |
| Opaque operator wallet | Public treasury + settlement timers |
| GH/s scoreboard only | GH/s steers Dig priority; sanitizer work stays CPU |

## Technology transparency

- Open source node, coordinator, worker binaries (`0.1.0-rc18`)
- Security research: Hunt Watch 12/12 · OSS CVE Watch (nghttp2 14/14 CLEAN · libheif 14/14 CLEAN)
- Marketplace fleet ETA / capacity: https://hackme.tech/fuzz-marketplace.html
- Policy regression locks in Go (`economics_test.go`)

## Integration pack

1. [EXCHANGE_LISTING_WALLET_PREP.md](EXCHANGE_LISTING_WALLET_PREP.md)
2. [spec/CHAIN_SPEC.md](../../spec/CHAIN_SPEC.md)
3. [docs/API.md](../API.md) — transfers section
4. Deposit test: `transfer_v1` + explorer confirmation

## Market / liquidity (honest)

- **Current stage:** early PoW network; **official pool live**; own spot desk live as paper soft  
- **Near-term focus:** desk UX + miners + audit customers — **not** paid mid-CEX listing  
- **Later CEX candidates (examples):** PoW-friendly venues when MM budget exists  
- **Liquidity:** no external MM engaged yet  
- **No ROI promises** in official channels

## Legal

- Risk disclosures: https://hackme.tech/legal-risk.html
- Not registered as a security offering; utility + mining infrastructure narrative
- Entity / counsel: **in progress** for Tier-1 exchanges

## Social proof

- Bitcointalk ANN topic 5583373
- Telegram: @hackme_tech
- GitHub releases: `0.1.0-rc18`
- Pool / research: live APIs + https://hackme.tech/listing.html

---

**Attachments (generate):** five branded PDFs — see [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md) and https://hackme.tech/listing.html
