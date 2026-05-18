# Multi-PC Coordinator Stress Runbook

Goal: validate coordinator consistency under concurrent workers and adverse conditions.

## Scale phases

- **A:** single coordinator hardening (caps, backpressure, reason-coded drops).
- **B:** edge proxy fan-in per region/site.
- **C:** shard coordinator by `worker_id` hash.
- **D:** async stats materialization for read-heavy dashboards.

In Phase A, treat `/api/global/metrics` as canonical GLOBAL source for all miner UIs.

## 0) Topology

- **Machine A (coordinator)**: `go run ./cmd/coordinator`
- **Machine B/C/... (workers)**: claim/submit loops against coordinator
- Optional command node proxy:
  - set `HACKME_POOL_COORDINATOR_URL=http://<A>:8081`

## 1) Coordinator startup (Machine A)

```bash
cd ~/Desktop/HackMe
export HACKME_COORDINATOR_ADDR="0.0.0.0:8081"
export HACKME_COORDINATOR_WORK_BATCH="2000000"
export HACKME_COORDINATOR_LEASE_SEC="20"
export HACKME_COORDINATOR_REWARD_PER_M_ATTEMPTS="0.001"
export HACKME_COORDINATOR_FOUND_BONUS_HMC="0.01"
go run ./cmd/coordinator
```

Baseline check:

```bash
curl -s http://127.0.0.1:8081/api/work/stats | jq
```

## 2) Worker smoke loop (Machine B/C)

Set once per worker:

```bash
COORD="http://<COORD_IP>:8081"
WID="worker-$(hostname)-$RANDOM"
```

Claim:

```bash
CLAIM=$(curl -s -X POST "$COORD/api/work/claim" -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WID\",\"batch_size\":2000000}")
echo "$CLAIM" | jq
BASE=$(echo "$CLAIM" | jq -r '.base_nonce')
BATCH=$(echo "$CLAIM" | jq -r '.batch_size')
WORK_ID=$(echo "$CLAIM" | jq -r '.work_id')
```

Submit (accepted, no hit):

```bash
curl -s -X POST "$COORD/api/work/submit" -H "Content-Type: application/json" -d "{
  \"worker_id\":\"$WID\",
  \"base_nonce\":$BASE,
  \"batch_size\":$BATCH,
  \"work_id\":\"$WORK_ID\",
  \"attempts\":1500000,
  \"found\":false,
  \"hashrate_gh_s\":120.0
}" | jq
```

Expected: `accepted=true`, `reason=""`, non-negative `payout_hmc`.

## 3) Targeted edge-case checks

### A) Work ID mismatch

- Submit valid range with wrong `work_id`.
- Expected: HTTP `409`, `reason="work_id_mismatch"`.

### B) Lease expiry

- Claim range, wait longer than `lease_sec`, then submit.
- Expected: HTTP `409`, `reason="lease_expired"`.

### C) Duplicate result hash

- Submit `found=true` with `result_hash="same-hash"` from worker1 (accepted).
- Submit another `found=true` with same `result_hash` from worker2.
- Expected second: HTTP `409`, `reason="duplicate_result_hash"`.

### D) Reissue path

- Let lease expire, then another worker claims.
- Expected claim response contains `"reissued": true` and same base range.

## 4) Monitoring commands (Machine A)

```bash
watch -n 2 'curl -s http://127.0.0.1:8081/api/work/stats | jq "{issued_ranges,reissued_ranges,submitted_items,found_hits,expired_leases,unknown_submits,stale_submits,rejected_submits,dedup_submits,accepted_attempts,total_payout_hmc,active_leases}"'
```

Optional worker ledger summary:

```bash
curl -s http://127.0.0.1:8081/api/work/stats | jq '.workers'
```

## 5) PASS criteria

- No crashes/panics under concurrent claims/submits.
- Edge-case reasons match expected values.
- Counters move consistently with executed scenarios.
- `accepted_attempts` and `total_payout_hmc` increase monotonically.
- `active_leases` drains/reissues as expected.
- `scheduler_mode` exposed and stable (`orders` when open tasks exist, otherwise `baseline`).
- `drop_reason_count` and `ack_latency_ms` present in `/api/work/stats`.

## 6) FAIL criteria

- inconsistent reason codes for the same scenario
- negative or non-monotonic payout/attempt counters
- lost leases/ranges without corresponding stats movement
- coordinator unresponsive or unstable under sustained load
