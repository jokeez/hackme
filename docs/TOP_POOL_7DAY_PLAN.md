# Top Pool 7-Day Plan

Goal: move from "stable engineering pool" to "top-pool claim with evidence".

## Day 0 baseline (now)

- Run:
  - `PROFILE=canary BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 bash scripts/ops/top_pool_readiness_gate.sh`
  - `PROFILE=top BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 bash scripts/ops/top_pool_readiness_gate.sh`
- Save both summaries from `reports/gates/top_pool_gate_*/summary.json`.

## Day 1-2: Scale workers safely

- Increase active workers to at least 5 (then 10+ by Day 3-4).
- Keep `settlement_healthcheck` green and no permission/nonce incidents.
- Run `fuzz_runtime_gate` canary loop and monitor for repeated PASS.

## Day 3-4: Hold throughput and payout stability

- Target sustained:
  - `workers_count >= 10`
  - `blocks_last_1h >= 100`
  - `observed_block_sec <= 35`
- Confirm `total_unpaid_hmc` stays within agreed cap and no drift between accrued/settled.

## Day 5-6: Reliability proof

- No critical incidents for 48h.
- Continue long soak (`mining_long_soak`) and fuzz canary.
- Re-run:
  - `final_release_oneclick.sh`
  - `fuzz_super_gate.sh`
  - `top_pool_readiness_gate.sh` in `PROFILE=top`

## Day 7: Go/No-Go

- "Top pool" claim is valid only when:
  1) `PROFILE=top` gate is PASS;
  2) 48h incident-free window;
  3) payout SLA is stable and publicly demonstrable.

## Operator note

- For single-node canonical topology, strict multi-peer checks are expected to fail by design.
- Do not block launch on strict peer reachability if your intended topology is single-node canonical + coordinator.
