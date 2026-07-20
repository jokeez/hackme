# Mirror VPS — what to buy and do today

**Updated:** 2026-07-20 · Hub: CLODO NL `132.243.112.100` (AS216154)

---

## Goal

A **warm standby** host — not a second mining hub, not the exchange — that can restore site + chain + coordinator if the primary is blocked, lost, or needs maintenance.

---

## What to buy (recommendation)

| Priority | Provider | SKU | Region | Why |
|----------|----------|-----|--------|-----|
| **Recommended** | [Hetzner Cloud](https://www.hetzner.com/cloud) | **CPX21** (3 vCPU AMD, 4 GB RAM, 80 GB NVMe) | **Falkenstein (DE)** or **Helsinki (FI)** | Different ASN (**AS24940**), ~€8.5/mo, stable network |
| Budget | Hetzner | **CX22** (2 vCPU, 4 GB, 40 GB) | same | ~€4–6/mo — enough for standby; less headroom |
| Alternative | OVH VPS | Starter 4 GB | Gravelines (FR) | AS16276 — geographic + provider diversity |

**Do not buy today:**

| SKU | Why wait |
|-----|----------|
| Second CLODO Amsterdam | Same ASN family as hub — weak failover story |
| Large HMS box | HMS still prelaunch |
| Exchange-api VPS | After mirror **restore drill** passes |

---

## Today’s 90-minute checklist

1. **Order Hetzner CPX21** (DE or FI) — Ubuntu 24.04, SSH key only, no password login.
2. **Bootstrap** (same pattern as hub):
   ```bash
   # on mirror as root
   adduser hackme && usermod -aG sudo hackme
   mkdir -p /opt/hackme/data /opt/hackme/scripts/ops
   ```
3. **SSH config** on desktop:
   ```
   Host hackme-mirror
     HostName <NEW_IP>
     User hackme
     IdentityFile ~/.ssh/id_ed25519
   ```
4. **First snapshot** (after hub is healthy):
   ```bash
   NODE_SSH=hackme-vps MIRROR_SSH=hackme-mirror bash scripts/ops/mirror_snapshot.sh
   ```
5. Install daily snapshot cron on operator machine:
   ```bash
   MIRROR_SSH=hackme-mirror bash scripts/ops/install_mirror_snapshot_cron.sh
   ```
5. **Do not point DNS** at mirror until restore drill succeeds.
6. Lower DNS TTL on `hackme.tech` to **300s** when drill is scheduled (W3 task).

---

## What mirror runs in v1

| Service | v1 standby |
|---------|------------|
| `hackme-node` | yes (stopped during snapshot apply) |
| `hackme-coordinator` | optional — can start on promote |
| nginx + static site | yes on promote |
| Settlement timers | **no** on mirror until cutover |
| Mining / leader PoH | **no** until explicit promote |

---

## Cutover (when needed)

1. Stop hub (or confirm dead).
2. On mirror: start node → coordinator → nginx with prod TLS (certbot).
3. DNS A → mirror IP.
4. Verify: `/api/status`, `/pool/coordinator/api/work/stats`, site 200.
5. Announce Telegram + rotate admin tokens if compromise suspected.

Target **RTO ≤ 4h manual** for summer; **RPO = last snapshot** (run snapshot daily once stable).

## Restore drill (required once)

```bash
MIRROR_SSH=hackme-mirror bash scripts/ops/prepare_mirror_prod_stack.sh
MIRROR_SSH=hackme-mirror bash scripts/ops/mirror_restore_drill.sh
```

For Cloudflare DNS switch helper (optional automation):

```bash
CF_API_TOKEN=... CF_ZONE_ID=... CF_RECORD_ID=... TARGET_IP=103.244.227.134 \
bash scripts/ops/cloudflare_dns_failover.sh
```

---

## Related

- [VPS_CAPACITY.md](VPS_CAPACITY.md)
- [NETWORK_MODEL.md](NETWORK_MODEL.md)
- `scripts/ops/mirror_snapshot.sh`
- Obsidian: `HackMe/Infra/VPS Mirror Buy Guide 2026-07-20.md`
