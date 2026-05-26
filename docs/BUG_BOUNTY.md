# Security rewards (bug bounty) — HackMe Network

**Language:** English (public policy)  
**Version:** `0.1.0-rc11i`  
**Payout asset:** on-chain **HMC** (discretionary, not a token sale or investment)

---

## Summary

We welcome **private** reports of security issues, exploitable bugs, and high-quality UI/UX problems.  
Rewards are paid in **HMC** after we verify and ship a fix (or document a mitigation).  
Amounts below are **maximum guidelines** for a _single_ accepted report — the team may pay less or decline.

**Report here (do not post 0-days publicly):** https://hackme.tech/contacts.html  
**GitHub security policy:** [SECURITY.md](../SECURITY.md)

---

## Reward tiers (HMC guidelines)

| Severity | Examples | Typical range (HMC) |
|----------|----------|---------------------|
| **Informational** | Typos in docs, cosmetic CSS, duplicate of a known issue | **0** (thanks / credit only) |
| **Low — UI/UX** | Broken layout, misleading label, wrong link on site (non-phishing) | **1 – 5** |
| **Low — functional** | Minor API error message, non-exploitable validation gap | **2 – 10** |
| **Medium** | Auth bypass on non-critical route, coordinator leak without fund loss, DoS with clear PoC | **10 – 40** |
| **High** | Steal operator token from misconfig PoC, settlement logic bug with fund impact, WASM sandbox escape PoC | **40 – 100** |
| **Critical** | Remote drain of treasury/coordinator accrual, consensus break, forge settlement to arbitrary wallet | **100 – 200** |

**Monthly program budget (operator discretion):** ~**200 HMC** total across all payouts — first-come for accepted reports until cap is reviewed.

> HMC has **no guaranteed market price**. Rewards are ecosystem grants, not salary or investment returns.

---

## In scope

- `hackme-node`, `hackme-coordinator`, `workerpoh` (official builds from https://hackme.tech/downloads.html)
- Public pool at https://hackme.tech (nginx, TLS, `/pool/coordinator`, `/pool/api`)
- Official static site (hackme.tech)
- WASM sandbox / PoH validation bugs with reproducible PoC
- Hybrid signer / settlement scripts (see repo `scripts/ops/`)

---

## Out of scope

- Phishing or fake domains (report URL — we takedown, no HMC for “finding” a scam copy)
- Social engineering, lost passwords, miner misconfiguration (`WORKER_PAYOUT_MAP` wrong)
- Issues already listed in [SECURITY_AUDIT_REDTEAM.md](SECURITY_AUDIT_REDTEAM.md) without a **new** exploit path
- Theoretical issues with no PoC, scanner output without impact
- Low hashrate / “I earned too little” economics disputes
- Third-party GPU drivers, OS malware on miner PC
- Testnet/local-only misconfiguration unless it affects production defaults

---

## How to report

1. Email / contact form: https://hackme.tech/contacts.html — subject: `Security report`
2. Include:
   - Component (node / coordinator / worker / site)
   - Steps to reproduce (commands, HTTP requests, version `0.1.0-rc11i` or commit hash)
   - Impact (who loses what)
   - Your **HMC-…** payout address (optional until accepted)
3. Allow **90 days** coordinated disclosure after a fix is deployed (we may agree on earlier public credit).

**Do not** open public GitHub issues for exploitable bugs.

---

## Payment rules

1. **Private first** — public exploit posts may reduce or void reward.
2. **One payout per root cause** — duplicates split at our discretion.
3. **Fix first** — payment after patch is on `main` / production VPS (we notify you).
4. **KYC** — not required for small HMC grants; large repeats may be limited.
5. **Taxes** — your responsibility.

---

## Hall of fame

With reporter permission we may list alias + severity on https://hackme.tech/security-rewards.html (no obligation).

---

## Official links

| Resource | URL |
|----------|-----|
| Website | https://hackme.tech |
| This policy (site) | https://hackme.tech/security-rewards.html |
| Source | https://github.com/jokeez/hackme |
| Bitcointalk ANN | https://bitcointalk.org/index.php?topic=5583373.0 |

---

*Operator reserves the right to change tiers, cap, or pause the program with notice on the website News feed.*
