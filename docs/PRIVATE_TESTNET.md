# HackMe Private Testnet Runbook

## 1. Goal

Check the transfer stream (`transfer_v1`) and basic P2P gossip in a controlled network of 3-10 nodes before going to the public Internet.

## 2. Minimum configuration

- On each node:
  - `HACKME_ADMIN_TOKEN=<strong-secret>`
  - `HACKME_P2P_TOKEN=<peer-shared-secret>`
- `HACKME_P2P_PEERS=http://ip1:8080,http://ip2:8080,...` (without yourself)
  - optional discovery:
    - `HACKME_P2P_DISCOVERY=1`
- `HACKME_P2P_ADVERTISE_URL=http://<lan-ip>:8080` (URL that the node advertises to neighbors)
    - discovery transitive: if a neighbor returned `announce_url`, the node can add it as `source=discovered` (with an internal cap limited to the total number of peers).
- Open access only between testnet nodes (firewall allowlist).
- HTTPS/reverse-proxy is preferably already at this phase.

## 3. Startup order

1. Raise node A, execute `POST /api/genesis`.
2. Raise nodes B/C/... with the same `chain_id` code version.
3. Make sure that `GET /api/p2p/peers` is not an empty minimum for some nodes.
4. Send several `POST /api/tx/send` on one node.
5. Check the appearance of tx on neighboring nodes via `GET /api/tx/pool`.
6. Start mining on one/several nodes and make sure that tx goes to `included`.

## 4. Health-checks

- `GET /api/status`:
- `tip_height` is growing,
  - `schema_version == schema_expected`.
- `GET /api/p2p/peers`:
- there are current `seen_at`,
  - no long communication gaps.
- `GET /api/tx/{hash}`:
- statuses predictably move to `pending -> included`.

### One-command preflight gate (recommended before pre_release)

```bash
ADMIN_TOKEN=... \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
scripts/ops/private_stage_gate.sh
```

Checks:
- schema/auth invariants (`/api/status`);
- presence of diagnostic fields sync-block (`/api/p2p/sync`);
- availability of the hardware report (`/api/reports/hardware?format=json`);
- health coordinator;
- optional freeze/backup (`DO_FREEZE=1`, `DO_BACKUP=1`).

## 5. Soak-test (8-24h)

- Load:
  - transfer tx batches (different senders),
  - parallel mining,
  - periodic restarts of 1-2 nodes.
- Checks:
  - no loss of tx-history,
  - nonce does not “break” after restart,
  - mempool does not overflow abnormally.

## 6. Incidents and recovery

- If one node is degraded:
  1. Stop the process.
  2. Make a copy of `data/hackme.db`.
  3. Restart with the same env.
  4. Check `GET /api/status` and `GET /api/p2p/peers`.
- If the desynchronization is critical:
  - isolate the node from peers,
  - save DB for analysis,
  - recreate from a reference image/clean database according to the operator’s decision.

## 7. Criteria for entering the public stage

- At least 24 hours soak without losing the integrity of tx-history.
- There are no unexplained nonce/balance discrepancies between test group nodes.
- Prepared rollback plan and alert checklist.
