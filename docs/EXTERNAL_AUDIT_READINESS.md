# External audit & CEX readiness pack

**Version:** 2026-07-22 · **Release:** `0.1.0-rc12w` (ISO `0.1.0-rc11s`) · **Operator contact:** https://hackme.tech/contacts.html

One-page index for **third-party security reviewers**, **listing desks**, and **due-diligence** teams. This is an **honest** snapshot — not a marketing claim of “fully audited”.

---

## 1. Executive verdict

| Area | Status | Notes |
|------|--------|-------|
| Public pool + coordinator | **Live** | Hybrid signer strict · public stats API |
| On-chain HMC settlement | **Live** | Operator timers + treasury autopilot |
| SUP on-chain accrual | **Live** | Separate settle timer |
| B2B PoH / fuzz orders | **Live** | Escrow + pool-distributed workers |
| OSS CVE Watch research | **Live** | Public ledgers · publish gates |
| HMS storage lane | **Prelaunch** | Not for miners or CEX |
| Native spot exchange | **Private demo only** | `hackme-exchange-demo` — not production |
| Third-party code audit | **Not completed** | This doc prepares evidence; no paid audit yet |
| Legal entity for Tier-1 CEX | **In progress** | See [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) |

**Readiness score (operator self-assessment):**

| Audience | Score | Blocker |
|----------|-------|---------|
| Security researcher / bug bounty | **~75%** | Formal audit report missing |
| Small CEX / non-KYC desk | **~55%** | MM, entity, mirror DR drill |
| Tier-1 CEX | **~25%** | Entity, audit, liquidity, 90d volume |

---

## 2. System boundaries (what to review)

```
┌─────────────────────────────────────────────────────────────┐
│ hackme.tech (HMC hub — primary VPS)                         │
│  nginx TLS · static site · node API · coordinator · settlement│
└─────────────────────────────────────────────────────────────┘
         ▲ miners / customers              ▲ read-only followers
         │                                 │
   workerpoh / workerfuzz            P2P optional / canonical HTTP
```

| Component | Repo path | Threat focus |
|-----------|-----------|--------------|
| Command node + chain | `main.go`, `internal/chain/` | Consensus, wallet credit, order escrow |
| Coordinator | `cmd/coordinator/` | Accrual abuse, hybrid signer, rate limits |
| Worker | `cmd/workerpoh/` | Client integrity (out of server scope) |
| Settlement ops | `scripts/ops/settle_worker_payouts.sh` | Nonce races, treasury drain |
| Fuzz marketplace | `fuzz_*.go`, `internal/poolfuzz/` | Escrow, settle outbox idempotency |
| Site | `web/site/` | XSS, misleading economics copy |

**Out of scope for hub audit:** `hackme-exchange-demo` (separate product tree), HMS heavy VPS (not live).

---

## 3. Security controls (implemented)

| Control | Evidence |
|---------|----------|
| Admin API auth | `HACKME_ADMIN_TOKEN` on mutating routes |
| Coordinator worker auth | Token + hybrid Ed25519 payloads |
| Self-register integrator default **off** | `integrator_auth.go` · smoke: register → 403 |
| HMS market orders admin-only | `hms_market_api.go` |
| Settlement single-writer | `flock` on `worker_settlement_state.json` |
| Genesis treasury drain cap | `treasury_bootstrap_guard.sh` · 25 HMC/24h routine |
| Subsidy visibility | `pool_subsidy_budget_snapshot.sh` · `subsidy_ratio` |
| Economics regression locks | `internal/chain/economics_test.go` |
| Release gates | `scripts/ops/repo_final_selfcheck.sh`, `public_release_readiness.sh` |
| Pool threat model | [POOL_SECURITY_THREATS_VERDICT.md](POOL_SECURITY_THREATS_VERDICT.md) |

Recent hardening (2026-07-20): settlement catch-up guard fix, HMS pay idempotency, settle outbox idempotent enqueue — commit `50f14c2`+ on `main`.

---

## 4. Economics transparency (auditor-critical)

Three layers — **do not conflate**:

| Layer | What moves HMC | Disclosure |
|-------|----------------|------------|
| Chain emission | PoH blocks → primary wallet | [ECONOMICS_DASHBOARD.md](ECONOMICS_DASHBOARD.md) · `/api/metrics` |
| Coordinator accrual | Off-chain `payout_hmc` per accepted work | `/api/work/stats` |
| Settlement | Transfers to miner `HMC-…` | `/api/worker/settlement` |

**Honest operator note:** In `baseline` mode (no open B2B orders), fleet accrual can exceed hub block emission; treasury may subsidize from disclosed genesis/dev wallet. See `pool_subsidy_budget_snapshot.sh` and [ORDER_ECONOMICS.md](ORDER_ECONOMICS.md).

| Wallet | Role | Disclosure |
|--------|------|------------|
| `HMC-719006d93916ad52` | Genesis / dev fee treasury | [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) |
| `HMC-381c0c5e2cfcc714` | Primary node + settlement float | On-chain explorer |

---

## 5. Evidence artifacts (reproducible)

Run on a clean checkout (no secrets in repo):

```bash
go test ./... -count=1
bash scripts/ops/public_release_readiness.sh
bash scripts/ops/run_pool_health_check.sh
bash scripts/ops/pool_subsidy_budget_snapshot.sh
bash scripts/ops/settlement_healthcheck.sh   # needs tokens locally
```

Public read-only (no auth):

```bash
curl -fsS https://hackme.tech/api/status | jq '{commit,schema_version,network_mode_active}'
curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats | jq '{pool_hashrate_gh_s,target_mod,reward_per_m}'
curl -fsS https://hackme.tech/api/worker/settlement | jq '{ok,fleet_unpaid_hmc,threshold_ready}'
```

Research integrity:

- OSS CVE Watch publish gate: `scripts/ops/publish_oss_cve_watch_day_finish.sh` (min 50M iter, 3600s)
- Public ledgers: https://hackme.tech/reports/oss-cve-watch/

---

## 6. Known gaps (do not hide from auditors)

| Gap | Risk | Mitigation plan |
|-----|------|-----------------|
| No paid third-party audit | Medium | Budget Q3; provide this pack + repo access |
| Single primary VPS (no mirror DR yet) | Medium | Mirror VPS + snapshot drill — see `scripts/ops/mirror_snapshot.sh` |
| Coordinator does not re-verify every nonce on `found=false` | Low–Med | Hybrid strict + lease caps; optional `payout_found_only` |
| JSON settlement state (not SQL) | Low | `flock`; single host policy |
| Baseline subsidy from treasury | Economic | `subsidy_ratio` monitoring; B2B orders fund miners via escrow |
| HMS / exchange not production | Confusion | Clear labels on site; HMS prelaunch |
| Entity / counsel incomplete | CEX blocker | In progress per listing memo |

---

## 7. CEX listing prerequisites checklist

Use with [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) and [EXCHANGE_LISTING_ROADMAP.md](EXCHANGE_LISTING_ROADMAP.md).

| # | Requirement | Status |
|---|-------------|--------|
| 1 | Public explorer + status API | ✅ |
| 2 | Documented tokenomics + genesis disclosure | ✅ |
| 3 | Official pool live + stats | ✅ |
| 4 | Deposit/withdraw integration spec | ✅ `transfer_v1` |
| 5 | Security policy + bug bounty page | ✅ |
| 6 | Mirror DR drill | ☐ target Aug 2026 |
| 7 | 30d measurable spot volume (native or partner) | ☐ |
| 8 | Market maker / liquidity plan | ☐ |
| 9 | Legal entity + listing counsel | ☐ |
| 10 | Paid security audit letter | ☐ |

**Recommended listing order:** HMC first → SUP companion → HMS only after storage go-live.

---

## 8. Infra & continuity

| Host | IP | ASN / org | Role |
|------|-----|-----------|------|
| Hub | `132.243.112.100` | AS216154 CLODO · NL | Primary |
| B2B customer node | `89.150.41.40` | (customer) | Fuzz escrow + bootstrap orders |
| Mirror (planned) | TBD | **≠ CLODO** e.g. Hetzner AS24940 | Warm standby |

Generate live inventory: `bash scripts/ops/vps_inventory_snapshot.sh`

---

## 9. What we will provide to an engaged auditor

1. Read-only GitHub access or tarball at agreed commit  
2. Sanitized nginx + systemd unit list (no tokens)  
3. `reports/pool-health-*` and `data/pool_subsidy_budget_state.json` samples  
4. Walkthrough: coordinator accrual → settlement → on-chain credit  
5. OSS CVE Watch methodology + raw session paths on request  

**We will not provide:** production seeds, admin tokens, or treasury private keys.

---

## 10. Related documents

| Doc | Audience |
|-----|----------|
| [SECURITY.md](SECURITY.md) | Threat model |
| [POOL_SECURITY_THREATS_VERDICT.md](POOL_SECURITY_THREATS_VERDICT.md) | Pool-specific |
| [PUBLIC_LAUNCH_VERDICT.md](PUBLIC_LAUNCH_VERDICT.md) | Product boundaries |
| [NETWORK_MODEL.md](NETWORK_MODEL.md) | Architecture |
| [BUG_BOUNTY.md](BUG_BOUNTY.md) | Researcher rewards |
| [OPERATIONS_MONITORING.md](OPERATIONS_MONITORING.md) | Operator runbooks |

**Obsidian ops mirror:** `Documents/Obsidian Vault/HackMe/Security/External Audit Readiness.md`
