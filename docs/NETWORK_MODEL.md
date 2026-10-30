# HackMe network model: VPS, pool, P2P

<div align="center">

**HackMe Network** · `0.1.0-rc17.4` (full bundle) · [Pool](https://hackme.tech/pool/explorer) · [Telegram](https://t.me/hackme_tech)

</div>

Briefly for operators and miners: **who writes blocks**, **how PCs participate**, **why P2P** and **how block rewards and pool payouts are related**. API details - [`docs/API.md`](API.md), economy - [`docs/ECONOMICS_DASHBOARD.md`](ECONOMICS_DASHBOARD.md).

---

## Why is it arranged this way?

Yes, for a **public pool** this scheme is usually **more convenient and predictable**:

- **It’s easier for the miner:** there is no need to raise a “full” node and local PoH as a second chain - just a worker + URL of the coordinator and canon (dashboard / scripts from `README.md`). Fewer steps and less chance of making a mistake in the config.
- **One source of truth for the API:** with `HACKME_PUBLIC_AUTHORITY_BASE` / canonical URL, everyone who is configured this way has **the same** answers for **height, tip, balance, mempool** with the command node - “my local block #102” vs “network on #10000” do not differ. This reduces confusion and disputes about the state of the network.
- **Security is easier to keep in mind:** the boundary is clearer - public **VPS** and its TLS/nginx, admin/coordinator/P2P tokens, and the home machine does not have to be an open server with a full chain. The attack surface of a “just miner” is smaller if he does not bind `0.0.0.0` and does not pull too much.

Important disclaimer: **“same block information for everyone”** in the sense of **same JSON from the canon** - yes, if everyone is looking at the canonical `GET /api/chain` / `GET /api/reports/blocks` / `GET /api/status`. **File `data/hackme.db` on disk** for a follower without P2P may **lag** behind in height; this doesn't break the pool, but Explorer/local block table on PC may show a different tail until the sync is done - see section 2 below.

The price of the model is **trust in the canon operator** (VPS) and transparency of the pool policy (coordinator + settlement); this is a deliberate trade-off for a controlled public launch.

---

## 1. Two layers: chain and pool

| Layer | What is this | Typical host |
|------|---------|----------------|
| **Command node** (chain node) | SQLite blockchain, `POST /api/genesis`, public tip/balance/mempool. **Staging/prod D1:** hub runs with `HACKME_CHAIN_LEADER_LOCAL_POH=0` and accepts pool empty finds via admin `POST /api/poh/solve-baseline` (subsidy → settlement primary wallet). `LOCAL_POH=1` is lab / emergency leader mining only. | **VPS** (staging / prod) |
| **Coordinator** | Work queue: `claim` / `submit` / `stats`, share-PPS accrual, optional **baseline relay** (`HACKME_POOL_BASELINE_RELAY=on`) that posts valid empty finds to the command node. Does **not** replace the chain ledger. | Often the same VPS (`go run ./cmd/coordinator`) |

A miner on a home PC in **worker-mode** connects to **coordinator** and to the **canonical API** of the node (via `HACKME_POOL_COORDINATOR_URL`, `HACKME_CANONICAL_CHAIN_URL` or output from `HACKME_PUBLIC_AUTHORITY_BASE`). Local WASM PoH on the participant is **disabled** - this is not a “second chain”, but a client of the pool.

---

## 2. P2P - optional

**P2P is not required** to use the pool and view the canonical balance/mempool via HTTP.

- Specify **`HACKME_PUBLIC_AUTHORITY_BASE`** (or explicitly **`HACKME_CANONICAL_CHAIN_URL`** + **`HACKME_POOL_COORDINATOR_URL`**) - the node will pull aggregates and height hints from the command node, even without `HACKME_P2P_PEERS` (see hints in `GET /api/status` in the `main.go` code).
- **P2P** (`HACKME_P2P_PEERS`, ...) is needed if you want the **local SQLite** on the follower to approach the network (sync blocks), and not just the UI/API in “canonical” mode.
- **`GET /api/tasks` (Orders tab in the dashboard):** the list **is substituted from the canon** if the first peer is specified in `HACKME_P2P_PEERS` **or** the base URL of the canon is configured (`HACKME_CANONICAL_CHAIN_URL` / output from the coordinator / `HACKME_PUBLIC_AUTHORITY_BASE`), and the request does not go into loopback to the same HTTP listener. To view **only** local SQLite (rare dev script), set **`HACKME_TASKS_LIST_LOCAL_ONLY=1`**.

Total: **you can mine through a pool** without P2P; **copy of the chain on disk** like the leader - with P2P or one-shot scripts (`follower_bootstrap_from_vps.sh`, `prefinal_public_sync.sh`, etc., see main `README.md`).

---

## 3. Who “generates blocks” and how the emission is divided

- **PoH blocks** are appended on the **command node**. Under **D1**, pool workers find empty Blake3 solutions; the coordinator relays them with `solve-baseline`; the node mints the next empty block. Header `miner_address` is the finding worker; **subsidy credits the node primary / settlement wallet**.
- **Emission rate** is limited by ASERT / `poh_target_mod` (~30s target). More workers do **not** multiply inflation; they compete for finds and earn via **share-PPS accrual → settlement**.
- Pool participants are **not** paid by auto-splitting each block onto each GPU — see [`MINER_POOL_ECONOMICS.md`](MINER_POOL_ECONOMICS.md) and [`ECONOMICS_DASHBOARD.md`](ECONOMICS_DASHBOARD.md).

Orders (`POST /api/tasks`) / Dig / Hunt — separate escrow rails (`solve-order`, PayRun / `run_floor`), not empty-block subsidy.

### Block difficulty (ASERT, from block 495 400)

From index **495 400** the PoH modulus `M` follows ASERT (the aserti3-2d scheme Bitcoin Cash uses),
replacing the old per-block micro step, 5-block window and wall-clock stall easing
([`internal/chain/asert.go`](../internal/chain/asert.go)):

```
M(n) = 17 500 000 · 2^((30·(h_parent − 495 399) − (t_parent − t_anchor)) / 3600)
```

- **Holds the schedule, not the last interval:** the long-run average is exactly 30 s per block, so
  2 880 blocks/day and halving dates are real (the old retarget drifted to ~27.7 s and oscillated).
- **Verifiable:** `M` is a pure integer function of block heights and timestamps — any follower or
  auditor recomputes it from headers (the old stall easing lived in node-local `meta`).
- **Half-life 1 h:** one hour ahead of schedule doubles `M`, one hour behind halves it.
- **Timestamp rules:** a block's timestamp never decreases and is at most 120 s ahead of the
  validator's clock.
- Consensus vectors: `internal/chain/testdata/asert_vectors.json` (1000 cases from an independent
  reference implementation) and `TestASERTConsensusVectors`.

---

## 4. Related docs

- Public miner path: **`README.md`**, **`docs/SETUP.md`**, worker-mode scripts under `scripts/ops/`.
- Economics: **`docs/ECONOMICS_DASHBOARD.md`**.
- HTTP surface: **`docs/API.md`**.

### Chain sync probe

**`scripts/ops/verify_chain_sync_snapshot.sh`** compares `GET /api/status`, `GET /api/metrics`, and optional `GET /api/global/metrics` (height, SQLite lag hints, `pool_target_mod` vs `work.target_mod`). Requires `curl` and `jq`:

```bash
LOCAL_BASE=http://127.0.0.1:8080 bash scripts/ops/verify_chain_sync_snapshot.sh
```

If the wording in the UI/site differs from this file - **source of truth for the product** consider the code (`pool.go`, `main.go`, `internal/chain`) and the current `README.md`; Synchronize this document when changing the model.
