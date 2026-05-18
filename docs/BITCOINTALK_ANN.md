# Bitcointalk ANN draft (English)

Copy-paste and adjust `[POOL_URL]`, release links, and contact before posting.

---

## [ANN] HackMe Network — useful PoW pool, WASM tasks, public coordinator

**Type:** Pool / infrastructure (not an ICO)  
**Website:** https://hackme.tech  
**Explorer:** https://hackme.tech/pool/explorer  
**Downloads:** https://hackme.tech/downloads.html  
**Source (when published):** `https://github.com/YOUR_ORG/hackme`  
**Version:** 0.1.0-rc9  

---

### What is HackMe?

HackMe is a **useful Proof-of-Work** stack: miners run a desktop **node + worker** that connects to a **public pool coordinator**, submits verifiable work ranges, and accrues **off-chain HMC** credits. Payouts to your wallet happen via **operator settlement** (on-chain transfers), not automatically inside every PoH block.

**Important — read before mining:**
- Pool rewards are tracked on the **coordinator** (accrual), then paid on-chain by settlement scripts.
- Block subsidies on the canonical chain go to the **producing node's primary wallet**, not split per GPU automatically.
- Public pool uses **found-only payout policy** and **strict hybrid signing** on the coordinator (see docs).

---

### Quick start (miner)

1. Download the Windows or Linux bundle from [Downloads](https://hackme.tech/downloads.html). Verify SHA256 from the same page.
2. Run `start_hackme_public_pool.bat` (Windows) or `scripts/ops/desktop_mode_up.sh` (Linux).
3. Set your payout address in `WORKER_PAYOUT_MAP` (see README) — must match operator map for settlement.
4. Open the dashboard at `http://127.0.0.1:8080` → **Mining** → **Start pool worker**.
5. Track accrual: dashboard **Unpaid worker accrual** and coordinator **Work payout** table.

**Public authority (default):** `HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech`

---

### Economics (short)

| Layer | What you earn |
|--------|----------------|
| Coordinator | Off-chain `payout_hmc` per accepted work (found hits + policy) |
| Chain | On-chain HMC only after **settlement** to your `HMC-…` address |
| Orders / fuzz | Separate product track (enterprise); not required for pool mining |

Details: https://hackme.tech/economics-model.html

---

### Security & transparency

- Open-source node, coordinator, and worker (Apache-2.0).
- Red-team checklist: `docs/SECURITY_AUDIT_REDTEAM.md` in the repository.
- Admin tokens required on production VPS; coordinator not exposed without authentication on mutating routes.
- Do not run a public coordinator without `HACKME_COORDINATOR_ADMIN_TOKEN`.

**Report security issues:** https://hackme.tech/contacts.html (responsible disclosure — no public exploit posts).

---

### Links

| Resource | URL |
|----------|-----|
| Landing | https://hackme.tech/ |
| Docs hub | https://hackme.tech/docs.html |
| API reference | https://hackme.tech/api-reference.html |
| Operator checklist | https://hackme.tech/operator-checklist.html |
| Pool explorer | https://hackme.tech/pool/explorer |

---

### Disclaimer

Mining and cryptocurrency involve risk. HackMe is experimental infrastructure (rc9). No investment advice. Run only software you have verified. Operators may change pool parameters (difficulty, payout policy) with notice on the website.

---

*Thread tags suggestion: `[ANN][POOL][CPU][GPU][WASM][POS]` — adjust to forum rules.*
