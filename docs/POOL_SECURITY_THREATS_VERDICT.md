# Threats to the pool: verdict on the code (honestly)

A comparison of “as it happens in classic articles” with **what HackMe actually does** today. Coordinator: `cmd/coordinator/work.go`. Settlement: `scripts/ops/settle_worker_payouts.sh`. Retarget chain: `internal/chain/retarget.go`. Hybrid signer: coordinator env (`HACKME_POOL_HYBRID_*`).

---

## 1. “Empty balls” / fake attempts (the worker is lying about attempts)

**Attack idea:** take lease, do not count, send submit with `found=false` and inflated `attempts`, receive `rewardPerM * attempts / 1e6`.

**What is the coordinator doing now**

- Payment for attempts at `found=false`: `paidAttempts` is taken from the request, **but limited** by `batch_size` lease (`attempts > req.BatchSize` is cut off) - see `submit()` in `work.go` (~744–750).
- **Cryptographic double-check of the entire range** (recalculation of PoH for each nonce in the batch) **no**. **only** branch **`found=true`** is recalculated and strictly checked: range `found_nonce`, `validFoundNonceV1` (eval % `target_mod`), dedup `found_nonce` / `result_hash`.
- Spam protection: **rate limit** claim/submit per worker and per IP, **bad strikes → temporary ban** for obviously bad submit reasons (`markSubmitOutcome`: incorrect work_id, signature, replay, incorrect found_nonce, etc.) - not for “slow honest”.
- Mode **`HACKME_COORDINATOR_PAYOUT_FOUND_ONLY`**: with `found=false` **`paidAttempts=0`** - then payment is only for actually proven hit + bonus. This is the **strongest** answer to fake attempts **without** full verification of each attempt (expensive).

**Verdict**

| Configuration | Fake attempts to pay “just for attempts” |
|--------------|-----------------------------------------------|
| Public pool with **hybrid strict** + signed payload | It's harder to fake **payload** without a key; but **the work itself** is still not recalculated nonce-for-nonce. |
| **`payout_found_only=1`** | **Almost closes** the “pay for air without a hit” scenario. |
| Default (`found` can be false, payment for attempts) | **Trust in the number of attempts** within the lease; economic risk - **operator** (limits, found-only, monitoring `total_payout_hmc` vs canon). |
| “Random 1% batch recheck” in code | **No** - not implemented. |

---

## 2. Settlement: double spend / racing parallel scripts

**Attack idea:** two settlement processes, nonce race, double payout before state update.

**What does `settle_worker_payouts.sh` do**

- State: **JSON file** (`STATE_FILE`), not SQLite with `SERIALIZABLE`.
- Logic: read coordinator → for worker, calculated delta → `POST /api/tx/send` from `next_nonce` from node → **after success** updated JSON via `jq` + `mv`.
- **Chain:** resending with the same nonce should be rejected by the node logic (if the first transaction is already in the pool/chain) - this is a **second milestone**, but not a replacement for atomicity.

**Repository improvements**

- The script **`scripts/ops/settle_worker_payouts.sh`** at the beginning takes **`flock`** to the file **`${STATE_FILE}.flock`**: the second parallel process on the same host **exits immediately** (typical overlap cron), without competing for the nonce and `jq`+`mv` state.

**Verdict**

| Mechanism | Eat? |
|----------|--------|
| Serializable TX in the settlement database | **None** (JSON file + curl) |
| Blocking “pending” on the worker in state until the end of tx | **No** (update after success) |
| Protection against two processes **on one host** with one `STATE_FILE` | **`flock`** - **yes** (`settle_worker_payouts.sh`) |
| Protection from two **different** hosts with one payer | **No** - do not run two settlements with one wallet without external coordination |

---

## 3. Difficulty Manipulation / `rewardAuto`

**Idea:** jump in command-node metrics → coordinator recalculated `rewardPerM` is unprofitable for the operator.

**What is**

- The coordinator periodically pulls `/api/metrics` from `HACKME_COORDINATOR_TARGET_SOURCE_URL`, updates `target_mod`, `base_reward_hmc`, and at **`reward_auto`** recalculates `rewardPerM = base_reward * 1e6 / target_mod` - see `refreshTargetMod` in `work.go` (~313–376).
- **On chain** retarget PoH uses **windows and step limits** (`poHRetargetMaxStepUp/Down`, micro-step) - `internal/chain/retarget.go` is **block complexity smoothing**, not the same as **worker rate smoothing** in the coordinator.

**Verdict**

- There is **no** separate “moving average rewardPerM for 100 blocks” in the coordinator; mitigation - **sampling frequency**, **retarget limits on the circuit**, manual **`HACKME_COORDINATOR_REWARD_PER_M_ATTEMPTS`** if auto is disabled.
- Risk “the pool pays more than the canon” - **operator**: compare `total_payout_hmc`, accumulation, policy `payout_found_only`, limits.

---

## 4. Theft/substitution of state settlement

**Idea:** disk access → substitution `worker_settlement_state.json` → repeated or someone else's payments.

**What is**

- The file is **not encrypted**; security = **OS rights**, separate systemd user, **backups**, do not store state on shared NFS without control.
- **Independent append-only audit** inside the script **is not carried out** (there is only stdout/journal when logging cron).

**Verdict**

- Requirements “state encryption + independent transaction log” - **partially on the operation side** (log `journalctl`, external SIEM, immutable backup). In the settlement code there is a **minimal** trace (hash tx in state after success).

---

## 5. Where to look for the operator

1. Public pool: consider **`HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1`** if paying primarily for hits is acceptable.  
2. Hybrid: docs by hybrid signer + smoke under `scripts/ops/`.  
3. Settlement: **one** cron, **flock**; monitoring `settlement_healthcheck.sh`; backup state.  
4. Reconciliation: accumulated payments of the coordinator vs balance/issue of the canon - manually or using monitoring scripts.

The document can be used to answer the question “Are we protected as under Article X?” — **only with the tables above**, without promises of missing mechanisms.
