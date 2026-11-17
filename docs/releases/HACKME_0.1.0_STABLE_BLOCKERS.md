# HackMe 0.1.0 stable — remaining blockers (post-rc20)

**Verdict (2026-11-17):** **Not ready to tag `0.1.0` stable.** Ship **`0.1.0-rc21`** next (gates + honesty), then stable only after the blockers below clear.

Channel today: `0.1.0-rc20` (`scripts/release/CURRENT_VERSION`).

## Fixed in this hardening pass (do not regress)

| Item | Gate |
|------|------|
| Windows `workerpoh-opencl.exe` soft-WARN → **fail-closed** | `make_release_bundle.sh` + Inno Setup requires the file |
| Dirty tree / `vcs.modified` only in full pipeline | `assert_clean_release_tree.sh` + bundle `assert_worker_fresh` |
| NVRTC missing while shipping CUDA | bundle **ERROR** (not WARN) |
| `ALLOW_SOFT_PICKUP=1` too easy | needs `HACKME_SOFT_PICKUP_REASON` (≥8); refused when claiming stable |
| Dashboard spendable fallback to local mirror when hub set | honors `hub_prepaid` / `hub_prepaid_unavailable` |
| Wallet chip “Live HMC/USDT” while desk is paper | **Paper ref mid (D0)** |
| `apt_deb_gate` swallowed with `\|\| true` | fail-closed when `BUILD_DEB=1` |
| No structural stable claim gate | `scripts/tests/stable_readiness_gate.sh` in full release |

## Blockers before tagging `0.1.0` (no `-rc`)

1. **L1 self-update signing (SEC-STABLE-01)** — Scaffold landed: ed25519 `latest.json.sig` sign/verify (`scripts/release/sign_latest_json.sh`, updater `HACKME_UPDATE_SIG_MODE`). **Still open for stable:** flip default from `warn` → `require`, keep signing key only in `.secrets/`, publish `.sig` on every cut, and do not claim signed self-update in notes until `require` is the default.
2. **Money-path E2E green without soft pickup** — run `rc20_customer_pool_e2e.sh` + hunt/demo against public hub with miners live; **no** `ALLOW_SOFT_PICKUP`. Wire a canary into release/rc-freeze when secrets + fleet are available.
3. **Exchange remains paper/D0** — OK to ship stable **with paper desk** only if release notes + site + dashboard all say paper/D0 (not “live CEX”). Live custody/matching is **not** required for `0.1.0` if clearly labeled; do **not** claim D1+.
4. **Release notes + site channel consistency** — `docs/releases/HACKME_0.1.0_RELEASE.md` must exist with paper/D0 disclosure; listing/roadmap/downloads must match `CURRENT_VERSION` (avoid stomping parallel site-banner work — coordinate cut).
5. **Treasury payout discipline** — bounty pays only from `HMC-719006d93916ad52` (unchanged ops rule).

## Deferred (acceptable for rc21 / early stable if labeled)

- Algebraic prepaid mint / `LOCAL_SIGN` break-glasses (lab-only; keep unset on prod)
- `HACKME_ORDERS_ALLOW_LOCAL_FALLBACK` (default off)
- Windows Hunt engines soft-skip (Linux-only coverage path; Dig/PoH OOTB)
- Cosign/minisign on every artifact (apt GPG covers `.deb` path)

## Recommended next version

| Cut | When |
|-----|------|
| **`0.1.0-rc21`** | After this hardening is on `main` + Windows OpenCL rebuild verified in a full local release |
| **`0.1.0`** | After blockers 1–4 above; `HACKME_CLAIM_STABLE=1 bash scripts/tests/stable_readiness_gate.sh` must PASS (today it correctly FAILs) |

Do **not** set `HACKME_CLAIM_STABLE=1` or tag `0.1.0` until the gate passes.
