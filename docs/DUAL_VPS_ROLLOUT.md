# Dual VPS Rollout (Node + Coordinator Split)

This runbook describes a pragmatic split for higher miner load:

- **NODE VPS**: canonical chain node + public site/explorer (`:18080`, `:80/:443`)
- **COORD VPS**: dedicated work coordinator (`:18081`)

## Why split now

When worker count grows, the largest bottleneck is burst claim/submit traffic.  
Separating coordinator from the canonical node removes resource contention and reduces latency spikes.

## Target topology

- Miners/participants:
  - work claim/submit -> `COORD_IP:18081`
  - read-only chain metrics -> `NODE_IP:18080`
- Canonical node:
  - `HACKME_POOL_COORDINATOR_URL=http://COORD_IP:18081`
- Public traffic:
  - website + explorer -> NODE via Nginx (`80/443`)

## Prerequisites

- Two Linux VPS with SSH access.
- Firewall baseline ready on both hosts.
- Tokens prepared:
  - `ADMIN_TOKEN` (node admin)
  - `P2P_TOKEN` (node p2p auth)
  - `COORD_ADMIN_TOKEN` (coordinator admin auth)

## One-command cutover helper

Use:

```bash
NODE_SSH="root@<NODE_IP>" \
COORD_SSH="root@<COORD_IP>" \
ADMIN_TOKEN="<node_admin_token>" \
P2P_TOKEN="<p2p_token>" \
COORD_ADMIN_TOKEN="<coord_admin_token>" \
DOMAIN="hackme.tech" \
bash scripts/ops/dual_vps_cutover.sh
```

The helper will:

1. Build and sync binaries to both hosts.
2. Configure and restart `hackme-coordinator.service` on COORD.
3. Configure and restart `hackme-node.service` on NODE.
4. Point node to remote coordinator URL.
5. Run smoke checks for `/api/work/stats`, `/api/status`, `/api/global/metrics`.

## Post-cutover checks

Run from your workstation:

```bash
curl -fsS "http://<COORD_IP>:18081/api/work/stats" | jq '{workers_count,claim_per_min,submit_per_min}'
curl -fsS "http://<NODE_IP>:18080/api/status" | jq '{tip_height,tip_hash,mining}'
curl -fsS "http://<NODE_IP>:18080/api/global/metrics" | jq '{ok,global_source,work:.work.workers_count}'
```

Expected:

- coordinator responds and increments worker stats under load;
- node tip continues moving;
- global metrics source includes coordinator-backed data.

## Firewall recommendations

On **NODE VPS**:

- allow: `22`, `80`, `443`, optionally `18080` for trusted ops
- deny public access to mutating/admin-only endpoints at edge

On **COORD VPS**:

- allow: `22`, `18081` only from trusted miner CIDRs (or Cloudflare Tunnel/WireGuard)
- deny everything else

## Rollback plan

If split causes issues:

1. On NODE, set `HACKME_POOL_COORDINATOR_URL=http://127.0.0.1:18081`.
2. Start local coordinator on NODE.
3. Restart `hackme-node.service` and `hackme-coordinator.service`.
4. Re-run smoke checks.

## Notes

- Keep coordinator and node tokens separate in production.
- Tune coordinator limits with env vars:
  - `HACKME_COORDINATOR_CLAIM_PER_MIN`
  - `HACKME_COORDINATOR_SUBMIT_PER_MIN`
  - `HACKME_COORDINATOR_MAX_ACTIVE_LEASES`
  - `HACKME_COORDINATOR_MAX_WORKERS`
