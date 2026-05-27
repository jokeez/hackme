# VPS capacity and multi-coin layout

Live reference host: **NL KVM 2 vCPU / 4 GB RAM / 80 GB NVMe** (~4 miners, ~4 GH/s pool as of 2026-05).

## Main VPS (hackme.tech)

| Role | Service |
|------|---------|
| HMC command node | `hackme-node` |
| Public pool | `hackme-coordinator` |
| TLS / site | `nginx` |
| HMC payout | `hackme-worker-settlement.timer` |
| SUP on-chain mint | `hackme-worker-sup-settlement.timer` |

Install settlement timers:

```bash
sudo bash /opt/hackme/scripts/ops/setup_worker_settlement_service.sh
```

## When current VPS is enough

- **4–30** active pool workers, moderate GH/s
- Load average **&lt; 2.5** on 2 CPU
- Swap **&lt; 1 GB** sustained
- Disk **&lt; 70%** (`/opt/hackme/data` chain DB)

## Upgrade triggers

| Signal | Action |
|--------|--------|
| Load **&gt; 2.5–3** for days | **4 vCPU / 8 GB RAM** |
| Swap **&gt; 1 GB** sustained | More RAM; drop panel stack (ispmanager/MySQL/Apache) from prod |
| **&gt; 50** workers or heavy fuzz on same host | Split coordinator or coin nodes |
| Disk **&gt; 70%** | Expand disk or prune logs |

## Multi-coin (future)

```text
Main VPS     — coordinator hub, HMC chain, nginx, settlement
Coin VPS X   — node + SQLite for coin X only, P2P to main
Coin VPS Y   — same pattern
```

Miners keep one pool URL; routing is internal.

## Ops checks

```bash
bash scripts/ops/sup_full_verdict_gate.sh
curl -sS https://hackme.tech/pool/coordinator/api/work/stats?details=0 | jq '.sup_policy,.total_payout_sup'
ssh hackme-vps 'uptime; free -h; systemctl list-timers hackme-worker*'
```
