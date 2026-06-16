# Token allocation & vesting — HackMe ecosystem

**Purpose:** listing-grade disclosure for HMC, SUP, and HMS (preview). Updated from on-chain constants and public operator policy. Not investment advice.

Live balances: [token-transparency.html](https://hackme.tech/token-transparency.html)

---

## HMC (HackMe Coin)

**Max supply:** 100,000,000 HMC

| Category | % of max | Amount (HMC) | Vesting / unlock |
|----------|----------|--------------|------------------|
| Genesis treasury (DevFee) | **0.05%** | **50,000** | **Unlocked at genesis** (block #0); public address |
| Miner + order emission | **~99.95%** | ~99,950,000 | **Continuous** via PoH halving + order escrow |
| Team pre-mine | **0%** | 0 | None declared |
| Investor SAFT | **0%** | 0 | None declared |

**Treasury address (public):** `HMC-719006d93916ad52` — balance visible on explorer.

**Unlock calendar (simplified):**

| Period | HMC entering circulation |
|--------|--------------------------|
| Genesis (done) | +50,000 treasury |
| Ongoing | PoH base emission (~120 blocks/h × reward_base) |
| Ongoing | Order escrow payouts (customer-funded, not inflation) |
| Per transfer | Fee burn tally reduces circulating over time |

**Top-holder note:** Early mining concentrates supply in active pool wallets; no hidden team cliff. Concentration decreases as emission spreads — publish top-10 holder report quarterly (operator commitment).

---

## SUP (HackMe Support)

**Max supply:** 21,000,000 SUP

| Category | % of max | Vesting |
|----------|----------|---------|
| Public / team pre-mine | **0%** | None |
| Miner pool emission | **100%** | Minted only via quality-gated pool work |

**No vesting cliffs** — SUP that exists was earned through coordinator rules or on-chain mint after settlement.

**Fleet cap:** daily global accrual limited vs HMC pool payout (anti-farm).

---

## HMS (HackMe Storage) — prelaunch

**Max supply:** 21,000,000 HMS

| Category | % of max | Amount (HMS) | Vesting |
|----------|----------|--------------|---------|
| Genesis treasury float | **0.5%** | **105,000** | At lane genesis (not live on hub yet) |
| Storage + seal emission | **99.5%** | ~20,895,000 | Per-epoch budget after seal |

See [HMS_TOKENOMICS.md](HMS_TOKENOMICS.md). **Do not trade HMS** until lane is live and genesis is announced.

---

## Cross-ecosystem principles

1. **Separate ledgers** — HMC, SUP, HMS do not share emission curves.
2. **Work-based SUP** — no click-to-claim.
3. **Customer-funded orders** — B2B HMC escrow is demand, not mint-and-dump.
4. **Public treasury** — genesis addresses published; no opaque multi-sig without disclosure.

## Red-flag checklist (Binance-style DD)

| Question | HackMe answer |
|----------|---------------|
| Large team allocation without lockup? | **No** declared team pre-mine on HMC/SUP |
| Opaque treasury? | **DevFee address public**; explorer balances |
| Circulating supply unknown? | **API + transparency page** |
| Weak utility? | **Mining + audits + fees**; SUP/HMS extend utility |
| Unverified inflation? | **Policy hash** in `/api/status`; Go regression locks |

## Document exports

Per-ticker PDF/DOCX packs: [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md)
