# RFC: HackMe Economics v2

| | |
|---|---|
| **Status** | Public design — soft announce / process lock |
| **Branch** | Implementation on `econ-v2`; activation is a later height switch |
| **Network today** | `0.1.0-rc18` · Blake3 PoH · ASERT · `SHARE_STRICT` share-PPS · solvency M |
| **Scope** | HMC emission, pool funding, order fee/burn, SUP utility, activation process |
| **Out of scope** | Airdrops, vesting giveaways, address redesign as a blocker |

> **One-line target:** at a published activation height, HMC becomes **0.1 HMC/block**, with **~4-year halvings from genesis**, a permanent **0.002 HMC tail**, **no max-supply fiction**, real burns, and a pool funded from **emission** — not perpetual treasury subsidy.

This document is the canonical public description of what changes and why. Related live specs today: [HMC_TOKENOMICS.md](HMC_TOKENOMICS.md), [ORDER_ECONOMICS.md](ORDER_ECONOMICS.md), [SUPPORT_COIN_UTILITY.md](SUPPORT_COIN_UTILITY.md), [MINER_POOL_ECONOMICS.md](MINER_POOL_ECONOMICS.md).

---

## 1. Why this is needed

### 1.1 Honest pool economics

Under solvency M and share-PPS, an honest fleet earns on the order of **tens of HMC/day**, not “print 100 HMC/day from nowhere.” The chain still mints **0.01 HMC per empty block** (~2880 blocks/day → ~28.8 HMC/day of *gross* empty-block emission). Today much of that coinbase can land on the **authority local PoH** path instead of the settlement wallet that pays miners.

**Without routing empty-block emission into settlement, the pool cannot be sustained by the chain.** Treasury top-ups are a bridge, not a monetary policy.

### 1.2 Clear long-run schedule

v1 used a short halving interval and a public **100M max supply** that does not match the intended long-run story (tail emission never hits a hard cap cleanly). v2 makes the schedule explicit:

- higher early reward so useful work and security audits can scale,
- slower halvings (~4 years),
- a permanent small tail so security budget never goes to zero,
- **no advertised hard max supply**.

### 1.3 Fees and burns that mean something

Order open already records platform fee + burn tallies. v2 tightens the story: **10% upfront** (5% burn / 5% platform), with a Dig soft-start window, and burn intensity that follows HMC halvings then freezes in the tail era. Burns are **real destruction**, not cosmetic metadata forever.

### 1.4 SUP as loyalty utility, not a second money printer

SUP stays a **support** coin: earned from honest HMC pool work, capped supply, used for **non-refundable fees** — never as Dig/Hunt escrow (escrow stays HMC).

### 1.5 Why not “flip at block 650000”

Picking a round height months in advance ignores live tip velocity, soak results, and operator readiness. v2 activation is:

1. **Publish this RFC + soft announce** (what you are reading).
2. Finish D1 soak with live relay (pool empty finds → settlement; hub `LOCAL_POH=0` — **already on staging hub**).
3. Ship the **height-switch package** (D9) on the release branch.
4. **Publish the concrete activation height** computed as:

   > **activation_height ≥ tip_at_announce + blocks_in_≥14_days**

   at ~30s target block time (≈ **40 320 blocks / 14 days**), with extra margin if soak needs it.

5. Activate only at that height — one package, not piecemeal merges of “0.1 only” or “SUP only.”

---

## 2. What changes (summary)

| Area | Today (v1 / rc18) | Economics v2 |
|------|-------------------|--------------|
| Empty PoH base reward | **0.01** HMC/block | **0.1** HMC/block |
| Halving interval | **2 102 400** (~2y) | **4 204 800** (~4y) from genesis |
| Tail | 0.002 floor after schedule | **0.002 forever** (security budget) |
| Max supply in public economics | Often 100M / misleading | **None** (remove hard-cap narrative) |
| Who captures empty coinbase | Often authority local PoH | **Settlement / pool float** (D1+D2) |
| Treasury auto-topup | Disabled / manual catch-up | Remains off; not a policy substitute |
| Order fee/burn | 5% fee + 10% burn tally (see order docs) | **10% upfront: 5% burn / 5% platform**; Dig soft-start then Hunt |
| SUP emission | Accrual % of HMC work (live) | On-switch: **0.002 SUP/block**, same halving heights as HMC, **no tail**, cap **21 000** |
| SUP spend | Spec evolving | **Non-refundable fee only** (1:1 vs HMC fee); escrow always HMC |
| Activation | n/a | **Announced height** after ≥14d notice — not a pre-chosen vanity height |
| Airdrops | n/a | **Out of scope** for this RFC |

Dig/Hunt **package prices and escrow splits** (20/80, 40/60, Hunt 50/50) are **unchanged** by this RFC unless a later demand signal says otherwise.

---

## 3. HMC emission model (v2)

### 3.1 Parameters

| Parameter | v2 value |
|-----------|----------|
| Initial base (empty PoH block) | **0.1 HMC** |
| Target block time | ~**30 seconds** (unchanged) |
| Blocks / day (nominal) | ~**2 880** |
| Gross empty-block emission (era 0) | ~**288 HMC/day** |
| Halving interval | **4 204 800** blocks (~4 years) |
| Epoch formula | `epoch = floor((height - 1) / halving_interval)` |
| Base reward | `max(0.002, 0.1 / 2^epoch)` HMC |
| Tail | **0.002 HMC/block** permanently after halvings reach it |
| Units | 1 HMC = 10⁸ units (unchanged) |
| Max supply | **Not defined** — do not publish a hard cap |

Order-linked blocks remain **escrow-funded** (customer prepaid), not free inflation on top of the empty-block schedule.

### 3.2 Implementation sketch

```text
if height < RewardV2ActivationHeight:
    use v1 schedule (0.01, 2_102_400, …)
else:
    use v2 schedule from genesis epoch math (0.1, 4_204_800, tail 0.002)
```

Public `economics` / metrics fields that imply a **100M max supply** are removed or set non-binding at the same height.

### 3.3 What miners should expect

Share-PPS + solvency M still bind **honest** payout to verified work and day emission budget. Raising base reward increases the **chain security budget** available to settlement; it does not mean “unbounded claim anything.” Accrual, CapChunk, and unpaid anchors remain part of the money path.

---

## 4. Pool paid from emission (D1 / D2)

### 4.1 Problem

If the hub produces most empty blocks via `HACKME_CHAIN_LEADER_LOCAL_POH=1`, coinbase accrues to the command-node path and **miners never see emission** unless operators manually top up settlement.

### 4.2 v2 rule

1. Authority **local WASM PoH is off** on the public hub (or every empty coinbase is equivalently routed to settlement — same economic outcome).
2. Verified pool **non-order** finds may relay `POST /api/poh/solve-baseline` so the empty block credits the **primary / settlement payer** wallet (not the worker address).
3. Coordinator gate: `HACKME_POOL_BASELINE_RELAY` =
   - `off` — default historically,
   - `dry` — count would-mint (`baseline_relay_would`) without POST (soak),
   - `on` — live mint path.
4. Auto treasury float scripts are **not** the long-term funding model; alert if settlement float covers fewer than N days of honest accrual.
5. **One** public start buffer transfer of **2000 HMC** (D3 locked) with tx hash in the activation announce — bridge only.

Orders continue to use `solve-order` → escrow to the **miner**.

### 4.3 Operator checklist before live `on`

- [x] Dry soak: `baseline_relay_would` tracks empty `found_hits` without reject storms  
- [x] Staging → live: local PoH off + relay `on`; tip keeps growing (`chain_leader_local_poh=false`)  
- [x] `baseline_relay_ok` vs `baseline_relay_reject` healthy (~3% reject; tip ~505k as of 2026-10-31)  
- [x] Settlement float + unpaid watched (`scripts/ops/d1_soak_watch.sh` + user timer); unpaid ≪ 200 HMC; **WARN** when primary float \< unpaid (thin between mint and sweep — expected under D1; activation buffer 2000 HMC closes the gap)  
- [x] Public note: treasury subsidy is not the policy (soft announce 2026-10-30)  

---

## 5. Orders, Dig/Hunt escrow, fee & burn (D6)

### 5.1 Unchanged product rails

| Rail | Escrow split | Notes |
|------|--------------|-------|
| Dig Scan / Audit | 20/80 | Package floors unchanged |
| Dig Deep | 40/60 | Unchanged |
| Hunt lite / standard / heavy | 50/50 | Unchanged; Hunt CLEAN floor pay stays fractional `run_floor` |

Customer prepaid Dig/Hunt budgets remain **HMC escrow**. That does not become SUP.

### 5.2 Fee / burn at order open (v2)

| Slice | Share of prepaid | Destination |
|-------|------------------|-------------|
| Burn | **5%** | Real supply destruction |
| Platform | **5%** | DevFee / platform |
| Working escrow | **90%** | Runs + bounty pools per package rules |

**Soft-start:** Dig-only for the first **~30 days** after activation, then Hunt (exact calendar in the activation announce). Burn **percentage** halves when HMC halves, then **freezes** in the tail era so fees do not asymptote to zero accounting noise.

PoH order prepaid (`reward_hmc × target_solves`) keeps existing minimums and pool-attach behavior unless called out in the activation notes.

---

## 6. SUP in v2 (D4 / D5)

| Topic | v2 choice |
|-------|-----------|
| Role | Loyalty / support — not a second settlement currency |
| Chain emit (from activation) | **0.002 SUP/block** |
| Halving heights | **Same block heights as HMC v2** |
| Tail | **None** (stops; no permanent SUP tail) |
| Cap | **21 000 SUP** (not 21M) |
| Earn today → through switch | Honest hybrid pool work remains the earn path; switch adds explicit block emit policy |
| Spend | **Non-refundable fees only** at 1:1 vs the HMC fee leg |
| Never | Dig/Hunt run/bounty escrow in SUP; faucet; airdrop |

Rough scale: ~2 880 × 0.002 ≈ **5.8 SUP/day** network-wide in era 0 before caps/halving — on the order of **one Hunt Standard fee per day** across the whole network, i.e. a loyalty bonus, not a parallel money supply.

Detail living doc: [SUPPORT_COIN_UTILITY.md](SUPPORT_COIN_UTILITY.md) (updated to match this RFC at activation).

---

## 7. Consensus & verifiability (with D9)

Same activation height packages:

- HMC v2 reward schedule  
- Order fee/burn policy  
- SUP emit + fee utility  
- `chain_id` binding appropriate to the switch  
- Mandatory authority block signature where required for hub blocks  

Nice-to-have, **not** blockers: new address format, every pool accrual as an on-chain tx (verify completeness debt).

Checkpoints (OTS + git) already run; post-activation work continues on dumps + `hackme-verify`.

---

## 8. Locked decisions (D1–D10)

| ID | Topic | Decision |
|----|-------|----------|
| **D1** | Who mines empty blocks | Retire hub local PoH (or equivalent); baseline relay funds settlement |
| **D2** | Pool funding | Coinbase → settlement + orders; no perpetual auto treasury topup |
| **D3** | Start buffer | One public **2000 HMC** transfer; hash in activation announce |
| **D4** | SUP emission | 0.002 SUP/block, shared halving heights, no tail, **hard cap 21 000** |
| **D5** | SUP utility | Non-refundable fee only; escrow always HMC |
| **D6** | Order fee/burn | 10% (5/5); Dig soft-start **30 calendar days** from activation announce, then Hunt |
| **D7** | Airdrops | **Out of scope** for this RFC / height switch |
| **D8** | Activation | tip-at-announce + **≥14 days**; height published before release — **not** a vanity block like 650 000 |
| **D9** | Minimal package | HMC schedule + fee/burn + SUP + `chain_id` + authority sig |
| **D10** | GPU / share harvest | **Done live:** `SHARE_STRICT` share-PPS; shares from full lease pass |

---

## 9. Activation process (read this before asking for a height)

```text
[ Soft announce: this RFC ]
        │
        ▼
[ D1 dry → staging → live relay ]  ← hub is on live relay (soak)
        │
        ▼
[ Merge/ship D9 height package on release branch ]
        │
        ▼
[ Activation announce ]
   - buffer tx hash (D3)
   - soft-start calendar (D6)
   - activation_height := f(tip, ≥14d, margin)
        │
        ▼
[ Wait ≥14 days · operators upgrade ]
        │
        ▼
[ Height switch — single package ]
```

**We will not** ship economics v2 by merging isolated numbers into `main` early. **We will not** freeze a block height in this RFC. The height comes in the **activation announce**, with enough wall-clock notice for miners, customers, and explorers.

---

## 10. Rollout status (engineering)

| Step | Status |
|------|--------|
| Blake3 PoH | ✅ live |
| SHARE_STRICT + solvency M | ✅ live (D10) |
| Accrual / settler / day budget | ✅ live |
| Checkpoints | ✅ live |
| Canonical settlement JSON | ✅ live |
| Dig/Hunt settle hardening (floor, finalize gate, replay) | ✅ on `econ-v2` |
| D1 baseline relay | ✅ hub live soak: `RELAY=on` + `LOCAL_POH=0`; ~2708 ok / 83 reject (~3%) · SHARE_STRICT on |
| Lock + public RFC (this doc) | ✅ |
| Soft announce (news + public docs) | ✅ 2026-10-30 |
| D9 height package implementation | ✅ on `econ-v2` (env bootstrap → locked `meta economics_v2_activation_height`; unset=off) |
| Activation announce + ≥14d | ⏳ |
| Height switch | ⏳ |
| Post-switch dumps / verify / 1.0.0 | after stability |

---

## 11. Locked answers (were open questions)

| # | Topic | Decision (2026-10-31) | Why |
|---|-------|------------------------|-----|
| 1 | Start buffer (D3) | **2000 HMC** | Covers ≥14d notice + soft-start while primary float stays thin under live D1 mint→sweep |
| 2 | Dig burn soft-start (D6) | **30 calendar days** from activation announce wall-clock | Clearer for customers/operators than a block-count; code constant `V2DigBurnSoftStartCalendarDays` |
| 3 | SUP cap (D4) | **21 000 hard cap** | Matches `V2SUPMaxSupply`; no “monitor ~16k” ambiguity for explorers/listings |
| 4 | Extra OP_RETURN | **No** for first switch | Checkpoints + OTS already; do not block activation |

---

## 12. Non-negotiables

1. **No piecemeal main merge** of emission knobs without the height package.  
2. **No vanity activation height** chosen months early.  
3. **No airdrop** as part of this economics switch.  
4. **No denomination** change (still 1e8 units).  
5. Halving interval changes **once** in v2; afterward the HMC schedule is permanent.  
6. Keep **mine + orders hybrid**; Dig/Hunt remain the useful-customer work lanes.  
7. Do not re-enable perpetual treasury printing as “policy.”

---

## 13. Why this design (short FAQ)

**Why 0.1 not 0.01?**  
Empty-block emission must be large enough to fund a real share-PPS pool once coinbase reaches settlement. 0.01 was a bootstrap; it is too tight once local PoH no longer silently “owns” the tip.

**Why ~4-year halvings?**  
Aligns long-run miner planning with a slower decay and a meaningful early era for useful compute growth.

**Why a tail?**  
A chain that secures customer escrow and pool settlement should not schedule security budget to absolute zero.

**Why no max supply?**  
A permanent tail contradicts a hard cap. Prefer honesty over a marketing number.

**Why SUP at all?**  
Loyalty and fee utility without contaminating HMC escrow or inventing a second PoH currency.

**Why ≥14 days notice?**  
Miners, desktop nodes, explorers, and customers need a predictable upgrade window. Tip-relative height tracks real network time better than a frozen 650k-style guess.

---

## 14. Document history

| Date | Note |
|------|------|
| 2026-10-23 | Internal draft economics v2 |
| 2026-10-29 | Reconciliation with live hub (shares, solvency, checkpoints) |
| 2026-10-29 | Public RFC: process lock, D10 marked live, D1 dry soak, **no airdrop scope**, activation height deferred to announce |
| 2026-10-30 | Soft announce on news + public docs; D1 flipped to live `on` + hub `LOCAL_POH=0` (soak continues; v2 height still deferred) |
| 2026-10-31 | Locked D3=2000 HMC, D6=30 calendar days, SUP cap=21k; D1 soak watch script+timer; D9 height package on `econ-v2` |

---

*HackMe Economics v2 — public RFC. Activation height will be published separately before the switch.*
