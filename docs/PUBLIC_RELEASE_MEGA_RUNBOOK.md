# Public Release Mega Runbook

Goal: run one consolidated pre-release validation pass for pool mode:

- health + canonical global metrics
- ultimate validation bundle
- p2p storm / DoS-style ingress pressure
- security assertions
- 51%-style worker dominance diagnostic

## 1) Start services (example)

```bash
cd /home/kapa/Desktop/HackMe
ADMIN_TOKEN=<admin_token> scripts/ops/stack_up.sh
```

If you run on VPS worker mode, keep canonical node/coordinator up first:

```bash
cd /opt/hackme
ADMIN_TOKEN=<admin_token> MAIN_BASE=http://127.0.0.1:18080 COORD_ADDR=0.0.0.0:18081 COORD_BASE=http://127.0.0.1:18081 START_CANON_MINING=1 bash scripts/ops/worker_mode_hub_up.sh
```

## 2) Run mega pack (quick default)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=public_quick_01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
P2P_TOKEN=<p2p_token> \
scripts/tests/public_release_mega_pack.sh
```

## 3) Run mega pack (aggressive pre-release)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=public_aggr_01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
P2P_TOKEN=<p2p_token> \
ULTIMATE_MEGA_DURATION=900 \
ULTIMATE_PRE_DURATION=1800 \
ULTIMATE_PRE_INTERVAL=60 \
P2P_STORM_REQUESTS=8000 \
P2P_STORM_CONCURRENCY=300 \
MAX_WORKER_DOMINANCE_PCT=51 \
scripts/tests/public_release_mega_pack.sh
```

## 4) Outputs

- `reports/tests/<RUN_ID>/public_release_mega/results.jsonl`
- `reports/tests/<RUN_ID>/public_release_mega/summary.json`

## 5) Gate policy

Release is blocked if any item fails:

- health endpoints unavailable
- `/api/global/metrics` unavailable
- ultimate bundle fails
- security assertions fail
- top worker dominates accepted attempts over configured threshold (`MAX_WORKER_DOMINANCE_PCT`)

## 6) Notes on 51% and exploit testing

- This pack adds a practical **dominance risk diagnostic** (worker concentration in coordinator payout flow).
- True consensus 51% resilience testing still requires multi-node adversarial scenarios with controlled partitioning/fork pressure.
- For deeper exploit assessment, add targeted fuzz/property campaigns and keep `fuzz_super_gate.sh` in release path.

