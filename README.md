<div align="center">

<pre aria-label="HackMe Network ASCII logo">
██╗  ██╗ █████╗  █████╗ ██╗  ██╗███╗   ███╗███████╗    ███╗   ██╗███████╗████████╗██╗    ██╗ ██████╗ ██████╗ ██╗  ██╗
██║  ██║██╔══██╗██╔════╝██║ ██╔╝████╗ ████║██╔════╝    ████╗  ██║██╔════╝╚══██╔══╝██║    ██║██╔═══██╗██╔══██╗██║ ██╔╝
███████║███████║██║     █████╔╝ ██╔████╔██║█████╗      ██╔██╗ ██║█████╗     ██║   ██║ █╗ ██║██║   ██║██████╔╝█████╔╝
██╔══██║██╔══██║██║     ██╔═██╗ ██║╚██╔╝██║██╔══╝      ██║╚██╗██║██╔══╝     ██║   ██║███╗██║██║   ██║██╔══██╗██╔═██╗
██║  ██║██║  ██║╚██████╗██║  ██╗██║ ╚═╝ ██║███████╗    ██║ ╚████║███████╗   ██║   ╚███╔███╔╝╚██████╔╝██║  ██║██║  ██╗
╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝╚═╝     ╚═╝╚══════╝    ╚═╝  ╚═══╝╚══════╝   ╚═╝    ╚══╝╚══╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝
</pre>

# HackMe Network

**Useful Proof-of-Work · public HTTP pool · GPU mining (CUDA / OpenCL)**

[![Release](https://img.shields.io/badge/release-0.1.0--rc11m-00d1ff?style=for-the-badge)](https://hackme.tech/downloads.html)
[![License](https://img.shields.io/badge/license-AGPL--3.0-39ff14?style=for-the-badge)](LICENSE)
[![Website](https://img.shields.io/badge/website-hackme.tech-7fe7ff?style=for-the-badge)](https://hackme.tech)

[Downloads](https://hackme.tech/downloads.html) · [Coins](https://hackme.tech/coins.html) · [Pool stats](https://hackme.tech/pool/coordinator/api/pool/stats) · [Explorer](https://hackme.tech/pool/explorer) · [Setup guide](docs/SETUP.md) · [All docs](docs/INDEX.md)

</div>

---

## What you get

HackMe is open mining infrastructure: a **desktop node** (dashboard at `:8080`) and a **pool worker** (`workerpoh`) submit work to the coordinator on [hackme.tech](https://hackme.tech). Rewards accrue as **off-chain HMC** until operator settlement.

| | |
|:---|:---|
| **Pool** | HTTP coordinator (not Stratum) · dynamic `target_mod` |
| **GPU** | NVIDIA CUDA · AMD/Intel OpenCL · CPU fallback |
| **Ecosystem** | **HMC** live pool · **SUP** accrual (hybrid HMC work) · **HMS** storage+seal lane (UI preview; backend on dedicated VPS #2 before go-live) |
| **Release** | `0.1.0-rc11n` — Windows/Linux + node watchdog + payments E2E; HackMe OS ISO still `rc11l` until next ISO build |
| **License** | [AGPL-3.0](LICENSE) · [Trademark](TRADEMARK.md) |

> Wallet balance on the dashboard ≠ pool payout until settlement. Map `WORKER_ID` → `HMC-…` with the operator. See [docs/NETWORK_MODEL.md](docs/NETWORK_MODEL.md).

---

## Quick start (5 minutes)

### Linux

```bash
git clone https://github.com/jokeez/hackme.git && cd hackme
cp .env.desktop.example .env.desktop    # edit WORKER_PAYOUT_MAP + optional tokens
bash scripts/ops/desktop_mode_up.sh
```

Open **http://127.0.0.1:8080** → **Workers** → start pool worker.  
Full steps: **[docs/SETUP.md](docs/SETUP.md)**

### Windows

1. [Download installer](https://hackme.tech/downloads.html) and verify SHA256 on that page  
2. Run **Start HackMe Miner**  

### HackMe OS (USB rig)

Flash ISO from downloads → boot **HackMe OS** menu → wallet + mining auto-start.  
Verify ISO: `bash scripts/tests/verify_hackme_iso.sh your.iso`

---

## Configuration files (do not commit secrets)

| File | Purpose |
|------|---------|
| `.env.desktop` | Local node + dashboard (`HACKME_ADMIN_TOKEN`, pool URL) |
| `.secrets/hackme_coordinator_worker_token` | Pool worker token (one line) |
| `.secrets/hackme_coordinator_admin_token` | **Operators only** — never publish |
| `HACKME_MINER_ED25519_SEED_HEX` | Per-rig signing key (`minersign -gen-seed`) |

Templates: `.env.desktop.example`, `scripts/ops/worker.env.example`.  
**Read before pushing to GitHub:** [docs/SECURITY_REPO.md](docs/SECURITY_REPO.md)

---

## Build from source

```bash
go build -trimpath -o hackme-node .
go build -trimpath -tags opencl -o workerpoh-opencl ./cmd/workerpoh
go build -trimpath -o hackme-coordinator ./cmd/coordinator
VERSION=0.1.0-rc11n bash scripts/release/make_release_bundle.sh
```

---

## Health checks

```bash
bash scripts/ops/verify_project_health.sh
bash scripts/tests/public_site_smoke.sh
bash scripts/ops/run_miner_launch_gate.sh    # operators — RC gate
```

Current RC status: [docs/STATUS.md](docs/STATUS.md)

---

## Documentation map

| | |
|--|--|
| [docs/SETUP.md](docs/SETUP.md) | Install paths (Linux / Windows / ISO / CLI) |
| [docs/INDEX.md](docs/INDEX.md) | Full doc index |
| [docs/OPEN_POOL_MINERS.md](docs/OPEN_POOL_MINERS.md) | Pool rules for miners |
| [docs/HMS_PUBLIC_ROADMAP.md](docs/HMS_PUBLIC_ROADMAP.md) | HackMe Storage (HMS) prelaunch roadmap |
| [docs/HMS_BACKEND.md](docs/HMS_BACKEND.md) | HMS coordinator, storage/seal workers, Stratum pilot |
| [docs/SUPPORT_COIN_UTILITY.md](docs/SUPPORT_COIN_UTILITY.md) | HackMe Support (SUP) accrual |
| [docs/GPU_MINING_BACKENDS.md](docs/GPU_MINING_BACKENDS.md) | GPU matrix |
| [docs/API.md](docs/API.md) | HTTP API |
| [scripts/release/README.md](scripts/release/README.md) | Release pipeline |

---

## Security

| | |
|--|--|
| Official site | **https://hackme.tech** only |
| Downloads | SHA256 on [downloads.html](https://hackme.tech/downloads.html) |
| Vulnerabilities | [contacts.html](https://hackme.tech/contacts.html) — no public 0-days |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Do not commit `.env`, `.secrets`, `data/`, or `logs/` ([`.gitignore`](.gitignore)).

---

## License

Copyright © 2026 HackMe contributors · [AGPL-3.0](LICENSE)
