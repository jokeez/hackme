# Exchanges: HMC Listing Roadmap

Technical package for integration: **[EXCHANGE_LISTING_WALLET_PREP.md](EXCHANGE_LISTING_WALLET_PREP.md)** · HTTP API: **`docs/API.md`** · spec: **`spec/CHAIN_SPEC.md`**

**Public materials (ecosystem):** [listing.html](https://hackme.tech/listing.html) · [token-transparency.html](https://hackme.tech/token-transparency.html) · [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) · [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) · PDF: [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md)

## Stage 0 - before application (now)

| Requirement | Status | Where to check |
|------------|--------|----------------|
| Public command node + explorer | ✅ | https://hackme.tech/pool/explorer |
| `GET /api/status` (genesis, economics, policy) | ✅ | https://hackme.tech/api/status |
| Open GitHub | ✅ | https://github.com/jokeez/hackme |
| Bitcointalk ANN | ✅ | topic 5583373 |
| Pool in action + stats API | ✅ | `.../pool/coordinator/api/pool/stats` |
| MiningPoolStats (HMC coin page) | ⚠️ Closed Jul 2026 | Hosted dashboard shut down — use pool stats API instead |
| Stable settlement on-chain | ✅ | `hackme-worker-settlement.timer` (30s) on hub VPS |

Check with one command:

```bash
PUBLIC_BASE=https://hackme.tech NODE_SSH=hackme-vps bash scripts/ops/mps_listing_readiness.sh --vps
```

## Stage 1 - starting PoW exchanges (order)

| Exchange | Why | Deadline Guide | Listing fee (guideline) |
|-------|--------|----------------|-------------------------|
| **[Xeggex](https://xeggex.com)** | Main audience of GPU/PoW miners | 3–14 days (paid) / weeks (voting) | ~$500–2000 USDT (check on the website) |
| **[NonKYC.io](https://nonkyc.io)** | Custom networks, fast technical support | 1–2 weeks | on request |
| **[TradeOgre](https://tradeogre.com)** | Classic low-cap PoW | queue / paid | moderate |
| **CoinEx** | Stage 2 - when there is volume from the first three | 1–2 months after volume | above |

Don’t aim at Binance/Bybit at the start - you need volume, audit, and a legal entity.

## Stage 2 - what to submit in the listing form

1. **Integration pack (ZIP or links)**
   - `spec/CHAIN_SPEC.md`
- `docs/API.md` (transfers section)
   - `docs/EXCHANGE_LISTING_WALLET_PREP.md`
- Link to explorer: https://hackme.tech/pool/explorer
   - Link to **MiningPoolStats** - https://miningpoolstats.app/coins/HMC (live)

2. **Description of the coin**
   - Useful PoW / PoH, WASM tasks, coordinator pool (not Stratum)
   - Max supply / halving – from `GET /api/status` → `economics`

3. **Social networks**
   - GitHub, Bitcointalk ANN, Telegram, Discord — https://hackme.tech/contacts.html

4. **Deposit test**
   - Separate exchange wallet (`minersign -gen-seed`)
   - Test `transfer_v1` + confirmation in explorer

## Stage 3 - after the first exchange

- Specify the market price of HMC in the dashboard calculator (HMC $ field)
- CoinGecko / CoinMarketCap - separate application (needs trading + independent nodes)
- Do not promise ROI in advertising - only live stats and calculator

## What is not available out of the box (do not promise to the exchange without development)

- Native **Stratum** TCP (we have HTTP coordinator + workerpoh)
- BEP-20/ERC-20 wrapper (native HMC circuit only)
- Light wallet in the App Store (desktop + worker binaries available)

## Link to MiningPoolStats

**miningpoolstats.app closed (Jul 2026)** — HMC page no longer live. Do not cite MPS in exchange forms until a replacement aggregator lists HMC.

- Use **pool stats API** as proof of live network: `https://hackme.tech/pool/coordinator/api/pool/stats`
- Optional: resubmit to [miningpoolstats.stream](https://miningpoolstats.stream) if moderators accept HTTP coordinator pools
- CEX outreach is **post-summer 2026** priority — after OSS CVE Watch + miner traction (see [roadmap.html](https://hackme.tech/roadmap.html))
