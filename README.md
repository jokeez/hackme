# HackMe

Useful Proof-of-Work mining infrastructure: a Go node with a web dashboard, public pool coordinator integration, WASM task validation, and optional GPU PoH.

| | |
|---|---|
| **Version** | `0.1.0-rc9` |
| **Website** | https://hackme.tech |
| **Pool explorer** | https://hackme.tech/pool/explorer |
| **Downloads** | https://hackme.tech/downloads.html |

## What it does

- **Node** — HTTP API + tabbed dashboard (`127.0.0.1:8080` by default), SQLite chain, genesis, wallet, orders, fuzz campaigns.
- **Pool worker** — connect to a coordinator (`HACKME_PUBLIC_AUTHORITY_BASE` or `HACKME_POOL_COORDINATOR_URL`), submit work ranges, accrue off-chain payout, settle on-chain via operator scripts.
- **Coordinator** — separate binary (`cmd/coordinator`) for claim/submit/stats, fleet caps, hybrid signing.
- **Public site** — static landing in `web/site/` (deployed separately from the node process).

Mining rewards on the **canonical chain** credit the producing node’s primary wallet. **Pool workers** earn via coordinator accrual and periodic settlement to addresses in `WORKER_PAYOUT_MAP` (not automatic per-GPU splits of every block). See [docs/ECONOMICS_DASHBOARD.md](docs/ECONOMICS_DASHBOARD.md).

## Quick start (Linux desktop / miner)

**Requirements:** Go 1.22+, `curl`, `jq` (for ops scripts). GPU builds need OpenCL or CUDA dev libraries.

```bash
# Clone, then from repo root:
go run .

# Or desktop profile (creates .env.desktop on first run):
bash scripts/ops/desktop_mode_up.sh
```

Open **http://127.0.0.1:8080** · default DB: `data/hackme.db` (gitignored).

**Public pool follower** (recommended for miners):

```bash
export HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
# Optional: WORKER_PAYOUT_MAP=worker-my-pc=HMC-...
bash scripts/ops/desktop_mode_up.sh
```

Start the pool worker from the **Mining** tab or:

```bash
WORKER_AUTOSTART=1 SKIP_TOOLCHAINS=1 bash scripts/ops/desktop_mode_up.sh
```

Stop:

```bash
bash scripts/ops/desktop_mode_stop.sh
```

**Windows:** use `dist/release_*` zip or build with `scripts/release/make_release_bundle.sh` — run `start_hackme_public_pool.bat` or `start_hackme_desktop_mode.bat` (see `RELEASE_QUICKSTART.md` in the bundle).

## Build

```bash
go build -trimpath -o hackme-node .

# GPU (OpenCL when available):
go build -trimpath -tags opencl -o hackme-node .
```

Coordinator:

```bash
go build -trimpath -o hackme-coordinator ./cmd/coordinator
```

Release bundle:

```bash
VERSION=0.1.0-rc9 bash scripts/release/make_release_bundle.sh
bash scripts/release/verify_artifacts.sh dist/release_0.1.0-rc9
```

## Documentation

| Document | Topic |
|----------|--------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components and data flow |
| [docs/NETWORK_MODEL.md](docs/NETWORK_MODEL.md) | VPS command node, coordinator, workers, P2P |
| [docs/API.md](docs/API.md) | HTTP API surface |
| [docs/SECURITY.md](docs/SECURITY.md) | Threat model, admin token, hardening |
| [docs/SECURITY_AUDIT_REDTEAM.md](docs/SECURITY_AUDIT_REDTEAM.md) | Pre–open-source red-team audit and production checklist |
| [docs/OPEN_POOL_MINERS.md](docs/OPEN_POOL_MINERS.md) | Joining the public pool |
| [docs/OPERATOR_FINAL_CHECKLIST.md](docs/OPERATOR_FINAL_CHECKLIST.md) | Production deploy gates |
| [docs/PUBLIC_LAUNCH_VERDICT.md](docs/PUBLIC_LAUNCH_VERDICT.md) | What is / is not guaranteed at launch |
| [scripts/release/README.md](scripts/release/README.md) | Release pipeline |

Static site sources: [web/site/](web/site/).

## CI and health checks

```bash
bash scripts/ops/verify_project_health.sh
bash scripts/ops/public_release_readiness.sh
```

GitHub Actions: [.github/workflows/ci.yml](.github/workflows/ci.yml) (`gofmt`, `go test`, `go vet`, language static checks).

## Security

- Set `HACKME_ADMIN_TOKEN` for mutating routes; never commit tokens or seeds.
- Ignored paths: `.secrets/`, `.env.desktop`, `data/*.db`, `logs/`.
- Pool settlement is operator-driven: `scripts/ops/settle_worker_payouts.sh` on the chain host.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0 — see [LICENSE](LICENSE).
