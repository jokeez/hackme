# Bitcointalk announcement — HackMe Network

**Language:** English only (forum post)  
**Version:** `0.1.0-rc11j`  
**Source:** https://github.com/jokeez/hackme  
**Live thread:** https://bitcointalk.org/index.php?topic=5583373.0

---

## Copy for the forum (Markdown-style preview)

Use the BBCode file for one-click paste: **[BITCOINTALK_ANN_BBCode.txt](BITCOINTALK_ANN_BBCode.txt)**

---

### Title (suggested)

`[ANN] HackMe Network — useful PoW pool · WASM work · public coordinator · rc9`

---

### Body

**HackMe Network** is open infrastructure for **useful Proof-of-Work**: a desktop **node + pool worker** connects to our **public coordinator**, submits verifiable nonce ranges, and accrues **off-chain HMC** credits. On-chain payouts reach your wallet after **operator settlement** (not inside every PoH block).

| | Official links |
|:---|:---|
| **Website** | https://hackme.tech |
| **Downloads** (SHA256 on page) | https://hackme.tech/downloads.html |
| **Pool explorer** | https://hackme.tech/pool/explorer |
| **Economics** | https://hackme.tech/economics-model.html |
| **Docs** | https://hackme.tech/docs.html |
| **Source** | https://github.com/jokeez/hackme (Apache-2.0) |

---

#### What you run

1. **Node** (`hackme-node`) — dashboard, wallet view, worker launcher (`:8080` locally).
2. **Worker** (`workerpoh`) — claims work from the coordinator, submits results.
3. **Optional:** follow the **canonical chain** read-only from `https://hackme.tech` (follower mode).

No ICO. No token sale. Pool / infrastructure thread.

---

#### Miner quick start

1. Download **Windows** or **Linux** bundle from [Downloads](https://hackme.tech/downloads.html).
2. **Verify SHA256** on the same page — do not use mirrors or random Telegram links.
3. **Windows:** run `start_hackme_public_pool.bat` from the zip.
4. **Linux:** clone the repo (or use the bundle) and run:
   ```bash
   export HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
   bash scripts/ops/desktop_mode_up.sh
   ```
5. Set **`WORKER_PAYOUT_MAP`** so your worker id maps to your **`HMC-…`** address (must match the operator map for settlement).
6. Open **http://127.0.0.1:8080** → **Mining** → **Start pool worker**.
7. Watch **Unpaid worker accrual** on the dashboard and the coordinator **Work payout** table on the explorer.

**Default public authority:** `HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech`

---

#### Economics (read this)

| Layer | What happens |
|--------|----------------|
| **Coordinator** | Off-chain `payout_hmc` for **accepted attempts** (fair mode: `reward_per_m` × attempts/1M + found bonus) |
| **Chain** | Block subsidy credits the **producing node’s primary wallet** — not auto-split to every GPU |
| **Settlement** | Operator script sends accumulated HMC to your on-chain address (~2 min timer) |

**Bottom line:** you earn while submitting accepted work — not only on rare block finds. Accrual ≠ wallet until settlement. Details: https://hackme.tech/economics-model.html

---

#### Security & trust

- **Official site only:** `https://hackme.tech` — check the URL bar.
- **Checksums:** every release on the downloads page; match before running binaries.
- **Open source:** Apache-2.0 — audit `docs/SECURITY_AUDIT_REDTEAM.md` in the repo.
- **Production pool:** admin token on coordinator, strict hybrid signing, hardened VPS (operator).
- **Do not** expose your own public coordinator without `HACKME_COORDINATOR_ADMIN_TOKEN`.

**Vulnerability reports:** https://hackme.tech/contacts.html — responsible disclosure only (no public 0-day dumps).

**Forks:** code may be forked under the license; the **HackMe name and logo** are not a free pass to impersonate the official pool. See `docs/TRADEMARK_AND_FORKING.md`.

---

#### Hardware

- **CPU:** supported (WASM PoH path).
- **GPU:** optional OpenCL/CUDA builds (`workerpoh` tags).
- **OS:** Linux and Windows release bundles.

---

#### Operator / advanced

| Resource | URL |
|----------|-----|
| API reference | https://hackme.tech/api-reference.html |
| Operator checklist | https://hackme.tech/operator-checklist.html |
| Network model (repo) | `docs/NETWORK_MODEL.md` |

---

#### Disclaimer

Cryptocurrency mining involves technical and financial risk. HackMe is **experimental** release-candidate software (`0.1.0-rc11j`). Not investment advice. Pool parameters (difficulty, payout policy) may change with notice on the website. Run only software you have verified.

---

#### Thread tags (adjust to forum rules)

`[ANN][POOL][CPU][GPU][WASM][ALGO]`

---

*Prepared for Bitcointalk — keep this file in sync with `README.md` and the live downloads page.*
