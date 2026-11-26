# HackMe 0.1.0 stable — remaining blockers (post-rc20, no rc21)

**Verdict (2026-11-26):** **Not ready to tag `0.1.0` stable.** There will be no `rc21` — the channel stays on **`0.1.0-rc20`** and the next cut is `0.1.0` itself, once the blockers below clear.

*(Superseded: the 2026-11-17 verdict recommended shipping an `rc21` first. That cut was dropped; its package has been removed from the apt repo, which now serves `rc20` only.)*

Channel today: `0.1.0-rc20` (`scripts/release/CURRENT_VERSION`).

## Fixed in this hardening pass (do not regress)

| Item | Gate |
|------|------|
| Windows `workerpoh-opencl.exe` soft-WARN → **fail-closed** | `make_release_bundle.sh` + Inno Setup requires the file |
| Dirty tree / `vcs.modified` only in full pipeline | `assert_clean_release_tree.sh` + bundle `assert_worker_fresh` |
| NVRTC missing while shipping CUDA | bundle **ERROR** (not WARN) |
| `ALLOW_SOFT_PICKUP=1` too easy | needs `HACKME_SOFT_PICKUP_REASON` (≥8); refused when claiming stable |
| Dashboard spendable fallback to local mirror when hub set | honors `hub_prepaid` / `hub_prepaid_unavailable` |
| Wallet chip “Live HMC/USDT” while desk is paper | **Desk mid (soft-launch)** |
| `apt_deb_gate` swallowed with `\|\| true` | fail-closed when `BUILD_DEB=1` |
| No structural stable claim gate | `scripts/tests/stable_readiness_gate.sh` in full release |

## Blockers before tagging `0.1.0` (no `-rc`)

1. **L1 self-update signing (SEC-STABLE-01)** — Scaffold landed: ed25519 `latest.json.sig` sign/verify (`scripts/release/sign_latest_json.sh`, updater `HACKME_UPDATE_SIG_MODE`). **Still open for stable:** flip default from `warn` → `require`, keep signing key only in `.secrets/`, publish `.sig` on every cut, and do not claim signed self-update in notes until `require` is the default.
2. **Money-path E2E green without soft pickup** — **cleared locally (2026-11-17):** pool + hunt + demo hard E2E PASS with `ALLOW_SOFT_PICKUP` unset against live hub (SSH tunnel `HACKME_CHAIN_BASE=http://127.0.0.1:28080` when public TLS flakes). Root fix: register coordinator claim with customer SKU price (not post-fee escrow) + coordinator floor honors `budget_hmc_price`. Proof: `reports/gates/money_path_e2e/{pool,hunt,demo}/VERDICT.json` via `scripts/tests/record_money_path_proof.sh`; gate checks `scripts/tests/lib/money_path_proof.py`.
3. **Exchange soft-launch labeling** — Matching + HMC/SUP custody are GO; site/dashboard/release notes must say **soft-launch desk** (not “live CEX”, not stale “paper/D0”). USDT `HOT_SEND=0` + Soft-MM tape honesty required in public copy. **Progress 2026-11-18:** ticker/index/listing/dashboard/rc20 notes/gate aligned; SPA chrome → SOFT-LAUNCH; residual is SPA redeploy + any remaining archive news.
4. **Release notes + site channel consistency** — `docs/releases/HACKME_0.1.0_RELEASE.md` must exist with soft-launch disclosure; listing/roadmap/downloads must match `CURRENT_VERSION` (avoid stomping parallel site-banner work — coordinate cut).
5. **Treasury payout discipline** — bounty pays only from `HMC-719006d93916ad52` (unchanged ops rule).

## Deferred (acceptable for early stable if labeled)

- Algebraic prepaid mint / `LOCAL_SIGN` break-glasses (lab-only; keep unset on prod)
- `HACKME_ORDERS_ALLOW_LOCAL_FALLBACK` (default off)
- Windows Hunt engines soft-skip (Linux-only coverage path; Dig/PoH OOTB)
- Cosign/minisign on every artifact (apt GPG covers `.deb` path)

## Recommended next version

| Cut | When |
|-----|------|
| **`0.1.0`** | After blockers 1–4 above; `HACKME_CLAIM_STABLE=1 bash scripts/tests/stable_readiness_gate.sh` must PASS |

**Where the gate stands (2026-11-26):** every structural check passes and the money-path
proof is accepted; the only remaining failure is blocker 1 — `HACKME_UPDATE_SIG_MODE`
must default to `require`.

**Do not flip that default first.** The verification key does not reach installs today:
it is absent from the published `.deb`, returns 404 on the site, and is not present on a
running node. `scripts/release/inject_updaters_into_dist.sh` places it in `dist/linux`
and `dist/windows`, but the published `rc20` does not carry it. Ship the key with a cut,
confirm an install can verify a signature, and only then make `require` the default —
otherwise self-update dies for everyone on the first run.

Do **not** set `HACKME_CLAIM_STABLE=1` or tag `0.1.0` until the gate passes.
