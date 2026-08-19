# HackMe network model: VPS, pool, P2P

<div align="center">

**HackMe Network** · `0.1.0-rc14x` · [Pool](https://hackme.tech/pool/explorer) · [Telegram](https://t.me/hackme_tech)

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
| **Command node** (chain node) | SQLite blockchain, `POST /api/genesis`, with local PoH allowed - **write PoH blocks** (`HACKME_CHAIN_LEADER_LOCAL_POH=1` only on the leader). A public “source of truth” for height/balance/mempool on the network. | **VPS** (staging / prod) |
| **Coordinator** | Work queue for workers: `claim` / `submit` / `stats`, accounting for attempts and **accrual** payments according to the pool policy. By itself **does not replace** recording blocks in a chain. | Often the same VPS or a separate process (`go run ./cmd/coordinator`) |

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

- **PoH blocks** in the canon are written by **the command node** that actually solves the problem and adds the block to **its** chain (on a public stack this is expected **VPS** with the leader script/mining enabled according to your deployment).
- **The emission rate** of the network is limited by the target block interval (retarget `poh_target_mod`, etc.) - thousands of workers **do not multiply** emission; they **share the probability/share** in the PoH model and in the coordinator accounting (see the “Capacity” section in [`ECONOMICS_DASHBOARD.md`](ECONOMICS_DASHBOARD.md)).
- **The base/order of the block reward** on the chain side goes to the **primary wallet** of the producer node (not “automatically for each GPU” of the remote rig). Pool participants receive a share through **coordinator accrual → settlement/on-chain** - this is **not** the same as directly splitting each block onto each GPU (explicitly on the `web/site/index.html` website and in `ECONOMICS_DASHBOARD.md`).

Orders (`POST /api/tasks`) - a separate layer: escrow and linking a reward to a task when an order is open.

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
