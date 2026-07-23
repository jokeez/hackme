# Pool release freeze — `1.0.0-pool`

Fixed state of the **pool product** (coordinator + worker + canonical overlay + dashboard UI) for operators and miners.

## What is included in the “pool final”

- **API Node Version:** `version` to `GET /api/status` = `1.0.0-pool` (`main.go`).
- **Economy:** consensus treasurer, genesis mint 50000 HMC on `DevFeeAddress`, `policy_hash` - see `internal/chain/economics.go`, `internal/block/genesis.go`.
- **Mining:** `POST /api/worker/start`, coordinator (`cmd/coordinator`), signed submissions, rate limits - see `cmd/coordinator/work.go`.
- **Network:** `HACKME_CANONICAL_CHAIN_URL`, `HACKME_POOL_COORDINATOR_URL`, optional **`HACKME_PUBLIC_AUTHORITY_BASE`** (one base command node → auto-fill canon and coordinator with empty env), optional P2P + `network_sync` in `/api/status`, background sync follower with `HACKME_P2P_BACKGROUND_SYNC_SEC` + `HACKME_P2P_SYNC_STATE_REPLAY_ENABLED`.
- **UI:** dashboard - pool bar (canon Δ, P2P lag, ledger policy), public mining readiness badges, actual genesis text, step 3 = worker or leader PoH.

## Limitations (honestly)

- P2P apply **does not** reproduce the full state of accounts; true for a wallet in public mode - **canonical HTTP**.
- HA coordinator / multi-master is **not** declared in the code.

## Freeze artifact

Run (creates a report in `reports/pool-freeze-<timestamp>/`):

```bash
bash scripts/ops/pool_release_freeze.sh
```

Full local checklist without secrets (vet + tests + audit + build; optional public GET):

```bash
bash scripts/ops/repo_final_selfcheck.sh
# with public command-node check:
PUBLIC_RO_BASE=https://hackme.tech bash scripts/ops/repo_final_selfcheck.sh

# full integration smoke (coordinator + node + worker loop, ~1–2 min):
RUN_LOCAL_STACK_SMOKE=1 bash scripts/ops/repo_final_selfcheck.sh
```

Manually: save the binary, `go version`, the output `GET /api/status` from the prod node (without secrets), and the git tag if there is a repository.

---

## Checking the repository (2026-05-12)

- `go build -trimpath` — OK  
- `go test ./... -count=1` — OK  
- `scripts/ops/pool_release_freeze.sh` - OK (artifact in `reports/pool-freeze-*`)

Next, only the **operational** steps on your side are required (below).

---

## Final verdict: what remains after the code

| Region | Status in code | What remains for you |
|--------|----------------|------------------|
| Pool (coordinator, worker, signatures, limits) | Done | Running processes, `HACKME_*` env, tokens |
| Canon + wallet in network mode | Done | Public command node URL, stable DNS/IP |
| Economics / genesis / `policy_hash` | Committed | New chain: pure `data/`, one `POST /api/genesis`, one build for everyone |
| P2P sync (blocks) | Done (optional) | Only enable state replay consciously; balance - from the canon |
| Full replay SQLite state | **Not** in MVP | Don't wait from code; reliance on canonical API |
| HA, pool sharding | **Not** in MVP | One VPS or manual failover |
| TLS, WAF, monitoring | Not in binary | Nginx/Caddy, alerts, backups `data/*.db` |
| Treasurer's key under `DevFeeAddress` (`HMC-719006d93916ad52`) | Not in git | Operator: `go run ./tools/gen_treasury_key` when changing treasury; seed only in `.secrets/` / vault, see **`docs/TREASURY_KEY.md`** |

**Result:** development “minimum for the pool” is closed; open tail - **infrastructure, secrets, chain launch process and marketing**, not unfinished kernel features.

---

## VPS: buy outright or release first?

Recommended order (minimum risk and money):

1. **First commit the release to the artifact** - there is already `1.0.0-pool`, freeze script, tests. It doesn't require a VPS.
2. **One inexpensive VPS (staging)** - raise the same compose/systemd, check workers from 2-3 machines, nginx, tokens. At the same time, edit your website/marketing **on the same URL or subdomain** (`pool-staging.…`).
3. **Prot-VPS ~1 week before the “public day”** - new chain (as you planned), clean database, genesis announcement, if necessary **different** IP/name for the “combat” command node, so as not to be confused with staging.

**It is not necessary to buy a “big” product VPS before marketing is ready:** you can carry out development and integration on staging; prod pay when there is a launch date and a flow of miners. If you **already have** a working public IP (as in README staging) - a new VPS is only needed when you decide to separate staging from prod or scale.

**In short:** do not block the “release” by purchasing a VPS; **block the public prod day** by having a stable prod node + new chain + ready-made announcement.

**Verdicts and limits of public launch (in one file):** see **`docs/PUBLIC_LAUNCH_VERDICT.md`**. Fast automatic cut: **`bash scripts/ops/public_release_readiness.sh`** (full merge level is still **`repo_final_selfcheck.sh`**).
