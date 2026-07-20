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

### Useful Proof-of-Work · GPU mining pool · B2B security fuzz · public research ledgers

**Hash power that does real work** — WASM-gated useful PoW, coordinator accrual, on-chain settlement, and production fuzz campaigns on one stack.

<br/>

[![Release](https://img.shields.io/badge/release-0.1.0--rc11s-00d1ff?style=for-the-badge&logo=semanticweb&logoColor=white)](https://hackme.tech/downloads.html)
[![Pool live](https://img.shields.io/badge/pool-LIVE-39ff14?style=for-the-badge&logo=serverless&logoColor=white)](https://hackme.tech/pool/coordinator/api/pool/stats)
[![Security](https://img.shields.io/badge/audit-16%2F16_PASS-39ff14?style=for-the-badge&logo=shield&logoColor=white)](docs/STATUS.md)
[![CI](https://github.com/jokeez/hackme/actions/workflows/ci.yml/badge.svg)](https://github.com/jokeez/hackme/actions/workflows/ci.yml)
[![CodeQL](https://github.com/jokeez/hackme/actions/workflows/codeql.yml/badge.svg)](https://github.com/jokeez/hackme/actions/workflows/codeql.yml)
[![License](https://img.shields.io/badge/license-AGPL--3.0-7fe7ff?style=for-the-badge&logo=gnu&logoColor=white)](LICENSE)
[![Site](https://img.shields.io/badge/hackme.tech-online-ff6b9d?style=for-the-badge&logo=firefoxbrowser&logoColor=white)](https://hackme.tech)

<br/>

**[⬇ Downloads](https://hackme.tech/downloads.html)** · **[⚡ Quick start](docs/QUICK_START.md)** · **[⛏ Mine](docs/SETUP.md)** · **[🛡 Developers / Fuzz](https://hackme.tech/developers.html)** · **[📊 Pool stats](https://hackme.tech/pool/coordinator/api/pool/stats)** · **[🔬 Research](https://hackme.tech/research.html)** · **[📖 Docs](docs/INDEX.md)**

<br/>

| | |
|:---:|:---|
| **Coordinator** | `https://hackme.tech/pool/coordinator` |
| **Explorer** | [explorer-lite.html](https://hackme.tech/explorer-lite.html) |
| **Pool stats** | [api/pool/stats](https://hackme.tech/pool/coordinator/api/pool/stats) |
| **Source** | [github.com/jokeez/hackme](https://github.com/jokeez/hackme) |

</div>

---

## Why HackMe

Most “mining” is a lottery with no output. **HackMe ties hashrate to verifiable work:**

| Pillar | What you get |
|--------|----------------|
| **⛏ Mine** | Public HTTP pool · NVIDIA CUDA / OpenCL / CPU · hybrid Ed25519 submits · **HMC** rewards + **SUP** loyalty lane |
| **🛡 Fuzz** | B2B campaigns (`wasm_only` → `wasm_native`) · escrow · `fuzz_report_v2` · pool-distributed workers |
| **🔬 Research** | Bitcoin30 series · OSS CVE hunt · [nghttp2 Watch complete](https://hackme.tech/reports/oss-cve-watch/day14.html) · [libheif Day 1/14](https://hackme.tech/research.html) running |

```mermaid
flowchart TB
  subgraph rigs["Your rigs"]
    GPU["workerpoh-cuda / OpenCL"]
    ISO["HackMe OS USB"]
    WIN["Windows one-click"]
  end
  subgraph hub["hackme.tech"]
    COORD["Pool coordinator"]
    NODE["Authority node + chain"]
    FUZZ["B2B fuzz + escrow"]
  end
  subgraph value["Outcomes"]
    HMC["HMC on-chain"]
    SUP["SUP accrual"]
    RPT["Security reports"]
    LEDGER["Public research HTML"]
  end
  rigs --> COORD
  COORD --> NODE
  FUZZ --> COORD
  COORD --> HMC
  COORD --> SUP
  FUZZ --> RPT
  NODE --> LEDGER
```

> **Fair economics:** payout follows **accepted work** (`reward_per_m` × attempts), not lottery blocks.  
> Coordinator accrual → operator settlement → your `HMC-…` address. Details: [NETWORK_MODEL.md](docs/NETWORK_MODEL.md).

---

## Live stack (operator snapshot)

| Area | Status | Notes |
|------|--------|-------|
| **HMC pool** | **Live** | Auto `target_mod` · **11 workers** · **~170 GH/s** (live) · hybrid signer strict · accept rate ~99.7% |
| **Settlement** | **Live** | HMC + SUP systemd timers + autopilot on canonical host |
| **B2B fuzz / PoH** | **Live** | Dashboard `#orders` · `workerfuzz` on hub · bootstrap PoH path (`pool_distributed` + `create_poh_order`) completing deep orders |
| **OSS CVE Watch · nghttp2** | **14/14 complete** | [day14.html](https://hackme.tech/reports/oss-cve-watch/day14.html) CLEAN · ~14.32B exec · ASAN=0 |
| **OSS CVE Watch · libheif** | **Day 1/14 running** | HEIF/AVIF `file_fuzzer` · 24h cadence · [research hub](https://hackme.tech/research.html) |
| **Win / Linux / ISO** | **rc11s** | SHA256 on [downloads](https://hackme.tech/downloads.html) |
| **Security gate** | **16/16 PASS** | Red-team scripts in `scripts/ops/` |
| **HMS storage** | Preview | Prelaunch — not a miner lane yet |

Full snapshot & health commands: **[docs/STATUS.md](docs/STATUS.md)**

```bash
# One-shot pool audit (operators)
bash scripts/ops/run_pool_health_check.sh
```

---

## Ecosystem lanes

| Coin / lane | Role | Status | Start here |
|-------------|------|--------|------------|
| **HMC** | Primary PoW + pool settlement | **Live** | [OPEN_POOL_MINERS.md](docs/OPEN_POOL_MINERS.md) |
| **SUP** | Support accrual while mining HMC | **Live** | [SUPPORT_COIN_UTILITY.md](docs/SUPPORT_COIN_UTILITY.md) |
| **B2B fuzz** | Paid security campaigns | **Live** | [FUZZ_PRODUCT_GUIDE.md](docs/FUZZ_PRODUCT_GUIDE.md) |
| **HMS** | Storage + seal epochs | Preview | [HMS_PUBLIC_ROADMAP.md](docs/HMS_PUBLIC_ROADMAP.md) |

---

## Quick start

<table>
<tr>
<td width="33%" valign="top">

### Linux

```bash
git clone https://github.com/jokeez/hackme.git
cd hackme
cp .env.desktop.example .env.desktop
bash scripts/ops/desktop_mode_up.sh
```

Open **http://127.0.0.1:8080** → **Workers** → start pool GPU.

[Full setup →](docs/SETUP.md)

</td>
<td width="33%" valign="top">

### Windows

1. [Download installer](https://hackme.tech/downloads.html)
2. Verify **SHA256** on that page
3. **Start HackMe Miner** from Start menu

[Windows guide →](docs/MINER_WINDOWS_ONE_CLICK.md)

</td>
<td width="33%" valign="top">

### HackMe OS

Flash **HackMe-OS** ISO from downloads → boot rig → wallet + mining autostart.

```bash
bash scripts/tests/verify_hackme_iso.sh your.iso
```

[OS guide →](docs/HACKME_OS.md)

</td>
</tr>
</table>

**GPU reference:** RTX 5060 Ti class ~30–40 GH/s · field RTX 5090 ~140 GH/s — see [GPU_MINING_BACKENDS.md](docs/GPU_MINING_BACKENDS.md).

---

## B2B security fuzz

| Tier | Depth | Use case |
|------|-------|----------|
| `wasm_only` | Fast WASM guard | CI smoke, daily gates |
| `wasm_native` | WASM → native confirm | Bounty-grade triage |
| `bytes_corpus` | Structured mutations | Deep corpus passes |

- [developers.html](https://hackme.tech/developers.html) — public landing  
- [CUSTOMER_FUZZ_DELIVERABLES.md](docs/CUSTOMER_FUZZ_DELIVERABLES.md) — reports & repro  
- Pool-distributed path: hub `workerfuzz` pulls campaigns; bootstrap helpers under `scripts/ops/bootstrap_customer/`  
- **20%** pool fee on escrow campaigns funds worker payouts

---

## Research & transparency

| Series | Public hub |
|--------|------------|
| **Bitcoin Core 30-day fuzz** | [bitcoin30.html](https://hackme.tech/reports/bitcoin30.html) |
| **OSS CVE matrix** | [oss-cve/](https://hackme.tech/reports/oss-cve/) |
| **OSS CVE Watch · nghttp2** (complete) | [oss-cve-watch/](https://hackme.tech/reports/oss-cve-watch/) · [Day 14 finale · CLEAN](https://hackme.tech/reports/oss-cve-watch/day14.html) |
| **OSS CVE Watch · libheif** (Day 1/14) | New series · HEIF/AVIF decode · 24h/day cadence · ledger at publish |
| **L1 / B2B case studies** | [research.html](https://hackme.tech/research.html) |

**nghttp2 series (closed):** Day 14 finale **3.03B** exec · 17.0h · ASAN=0 · Days 2–14 cum ≈ **14.32B** — [series verdict](docs/verdicts/OSS_CVE_WATCH_NGHTTP2_SERIES_VERDICT.md).

**libheif series (active):** Day **1/14** · upstream `file_fuzzer` · libde265+dav1d · 24/7 fixed 86400s windows — [runbook](docs/OSS_CVE_LIBHEIF_SERIES.md).

Run locally: `DAY=8 bash scripts/ops/run_bitcoin30_day.sh` · [BITCOIN30_SERIES.md](docs/BITCOIN30_SERIES.md)

---

## Build & verify

```bash
go build -trimpath -o hackme-node .
go build -trimpath -tags opencl -o workerpoh-opencl ./cmd/workerpoh
go test ./...
bash scripts/tests/public_site_smoke.sh
bash scripts/tests/version_consistency_gate.sh
```

Release bundle: `VERSION=0.1.0-rc11s bash scripts/release/make_release_bundle.sh` — [scripts/release/README.md](scripts/release/README.md)

---

## Configuration

| File | Purpose |
|------|---------|
| `.env.desktop` | Local node + dashboard |
| `hackme.env` | Windows miner (installer) |
| `.secrets/*` | **Never commit** — tokens & seeds |
| `WORKER_PAYOUT_MAP` | `worker_id=HMC-…` for settlement |

→ [SECURITY_REPO.md](docs/SECURITY_REPO.md) before every push

---

## Documentation map

| Doc | Audience |
|-----|----------|
| [docs/INDEX.md](docs/INDEX.md) | Full map |
| [docs/SETUP.md](docs/SETUP.md) | Miners |
| [docs/STATUS.md](docs/STATUS.md) | Operators — **is prod OK?** |
| [docs/API.md](docs/API.md) | Integrators |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System design |

---

## Security & trust

| | |
|--|--|
| **Official site** | https://hackme.tech only |
| **Downloads** | SHA256 on [downloads.html](https://hackme.tech/downloads.html) |
| **Disclosure** | [contacts.html](https://hackme.tech/contacts.html) |
| **Bug bounty** | [BUG_BOUNTY.md](docs/BUG_BOUNTY.md) |

---

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) — AGPL-3.0 · respect [TRADEMARK.md](TRADEMARK.md) · no secrets in git.

---

<div align="center">

**HackMe Network** — useful work, open code, honest economics.

<br/>

[![Telegram](https://img.shields.io/badge/Telegram-@hackme__tech-26A5E4?style=flat-square&logo=telegram)](https://t.me/hackme_tech)
[![Bitcointalk](https://img.shields.io/badge/Bitcointalk-ANN-f7931a?style=flat-square)](https://bitcointalk.org/index.php?topic=5583373.0)

<br/>

<sub>Copyright © 2026 HackMe contributors · <a href="LICENSE">AGPL-3.0</a></sub>

</div>
