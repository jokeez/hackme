# HackMe - security model (MVP / local node)

Honest expectations: what is protected now, what is deliberately out of MVP scope, and what changes when the node is reachable beyond localhost.

This is not a “bank-like” audit or a legal guarantee.

---

## 1. Current trust model

| Aspect | Behavior |
|--------|-----------|
| Network | The HTTP server listens to **`127.0.0.1`** - the remote Internet **will not** reach the API until you forward the port / reverse-proxy yourself. |
| OS user | Any process **on your behalf** on the same machine can call `http://127.0.0.1:8080/...` just like a browser. |
| "Wallet" | One line **`wallet`** in SQLite (`balance_hmc`) - **accounting within a node**, not a separate hardware/HSM wallet. |
| Key **Ed25519** (`data/node_ed25519.seed`) | Signing of API responses (for example, orders), access rights to the file are with the OS user. |
| Dashboard | Static page with `localhost`. The Admin token is **not** embedded in HTML and **not** given to `/api/desktop/local-auth` unless explicitly set to **`HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1`** (loopback + desktop mode only). Otherwise, the token is set locally on the client (`sessionStorage`). |

---

## 2. What already exists (threat migration)

- **Admin token policy:** by default launch requires **`HACKME_ADMIN_TOKEN`** (`HACKME_REQUIRE_ADMIN_TOKEN=1`). Explicit mitigation for local debugging only: `HACKME_REQUIRE_ADMIN_TOKEN=0`.
- Mutating **POST** require a **`X-Hackme-Admin-Token: <token>`** or **`Authorization: Bearer <token>`** header. In case of error - **401 Unauthorized** with `WWW-Authenticate`.
- Defended by: **`POST /api/genesis`**, **`POST /api/mining/start`**, **`POST /api/mining/stop`**, **`POST /api/worker/start`**, **`POST /api/worker/stop`**, **`POST /api/tasks`**, **`POST /api/tasks/from_code`**, **`POST /api/push_work`** (body up to **1 MiB**, as for other large JSON), **`POST /api/hardware/tune`**, as well as the admin branches **fuzz** (see `fuzz_campaigns.go`). Reading (GET metrics, chain, logs, SSE logs, **`GET /api/hardware/tune`**, **`GET /api/worker/status`**) - without a token.
- For private testnet P2P: given **`HACKME_P2P_TOKEN`** endpoints **`/api/p2p/*`** require `X-Hackme-P2P-Token`.
- A basic rate-limit has been introduced for anti-spam: `POST /api/tx/send`, `POST /api/tasks`, `POST /api/p2p/tx`, `POST /api/push_work` get **429** when there is a surge.
- Added anti-drain limits on escrow orders: `HACKME_MAX_ORDER_ESCROW_PER_HOUR_HMC` (default limit is per hour).
- **SQLite `PRAGMA user_version`** — schema version after migrations; in **`GET /api/status`**: `schema_version`, `schema_expected`.
- **WASM:** timeout for calls, check-module size limit, wazero runtime memory limit (see `internal/sandbox`). Order files: only under **`tasks/artifacts/`** (or **`HACKME_TASK_ARTIFACT_DIR`**), relative path without `..`.
- **WASM hardening:** strict check of module header/version, `check(i64)->i32` export only, test call during validation, quarantine of invalid modules by hash (by default, re-download of quarantined hash is blocked). The start section and excessive **table/element** sections are rejected (H44). Settings: `HACKME_SANDBOX_MAX_CHECK_WASM_BYTES`, `HACKME_SANDBOX_CHECK_TIMEOUT_MS`, `HACKME_SANDBOX_BLOCK_QUARANTINED`.
- **`POST /api/tasks/from_code`:** the compiler, if possible, runs under **bwrap** / **nsjail** (`wrapCompilerCmd`) with **narrow** RO-bind (toolchain + workdir), **without** `--ro-bind / /` (otherwise `include_str!("/etc/...")` flows into `compile_log`). Without sandbox helper - **host compile - only lab**. Gate: **`HACKME_FROM_CODE`** (`0`/`1`; if not specified, enabled only on loopback bind). Cont/VPS: **`HACKME_FROM_CODE=0`**, plus **`HACKME_FROM_CODE_REQUIRE_SANDBOX=1`** if compile is still needed locally.
- **`.gitignore`:** `data/node_ed25519.seed` / `*.seed*` — do not commit keys.
- **History purge (2026-07):** a legacy backup `data/node_ed25519.seed.backup.dad0430-…` was briefly committed then deleted from the tree; the blob was later **removed from git history**. Derived address `HMC-dad0430ba660b7d5` is **not** the live node or treasury (`HMC-719006d93916ad52`). Do not reuse that seed; treat it as burned.
- **PoH blocks/sync:** When writing a block, the consistency of the `hash` field with the header (`verifyBlockIntegrityAndSignature` in `internal/chain/service.go`) is checked. The signature of the miner **Ed25519** is verified against the message **`hash` of the block** if the signature fields are specified; **completely empty** signature fields are allowed for compatibility with unsigned historical chains. On the P2P staging/apply path, if there is a signature, the same logic applies (`verifySyncBlockSignature` to `main.go`); fake `hash` or signatures for a different key are cut off. Tightening direction: optional mode “all new blocks signed only” (separate task/flag).
- **Local WASM PoH over HTTP:** only if the process is started from **`HACKME_CHAIN_LEADER_LOCAL_POH=1`** (command-node). Regular nodes/pool members mine through **`POST /api/worker/start`** and **`HACKME_POOL_COORDINATOR_URL`**. **`HACKME_BEGINNER_SOLO`** deleted (see `docs/BEGINNER_SOLO.md`).

---

## 3. Deliberately not in MVP (we don’t promise before the network)

- P2P authentication, protection against replay between nodes, consensus on “foreign” blocks.
- TLS on HTTP (for pure localhost it is often redundant; with a proxy - TLS on the proxy).
- Rate limiting / WAF on API.
- Full p2p authentication and peer reputation (currently baseline handshake + token).
- SQLite encryption “on disk” without a user password does little against the same OS user.
- Order cancellations and escrow returns are a separate economic and protocol model (see [`ORDER_ECONOMICS.md`](ORDER_ECONOMICS.md) and the public [roadmap](../web/site/roadmap.html)).

Demo / current emission policy:

- Genesis mints **50 000 HMC** once to the consensus treasury address `DevFeeAddress` (`internal/chain/economics.go`).
- Further emission is allowed via validated PoH block rewards and order flows under the supply cap.

---

## 4. Exposure beyond localhost

When the bind address is not loopback-only, the threat model widens:

| Area | Notes |
|------|--------|
| **Surface** | Who can reach TCP (VPN, LAN, or public IP). |
| **Transport** | TLS (or mTLS) on a reverse proxy; optional **`HACKME_HTTP_CORS_ALLOW_ORIGIN`** only when cross-origin browser access to `/api/*` is intentional. |
| **Authentication** | Do not embed `HACKME_ADMIN_TOKEN` in HTML; production needs roles / short-lived tokens. |
| **Secrets** | Dedicated OS user; tight permissions on `data/*.db` and `*.seed`. |
| **Limits** | POST body size, request rate, WASM timeouts, reject dangerous imports. |
| **Observability** | Logs without token leakage; alerts on abnormal escrow burn. |
| **P2P** | Peer identity, anti-replay, signed blocks when P2P is exposed. |
| **Transfers** | Signature + nonce anti-replay; watch `429` / `invalid_signature` / `invalid_nonce`. |

Helper: `scripts/ops/internet_preflight.sh` records sandbox/economics/status, security headers, difficulty health, and coordinator readiness under `reports/gates/<run_id>`.

---

## 5. Linked files

| File | Meaning |
|------|--------|
| `admin_auth.go` | Check `HACKME_ADMIN_TOKEN` |
| `main.go`, `pool.go` | Routes, call `requireAdminAuth` |
| `internal/store/sqlite.go` | Schema version |
| `internal/nodecrypto/` | API Signing Key |
| `spec/CHAIN_SPEC.md`, `docs/API.md` | Protocol and HTTP |

**Separate process `cmd/coordinator`:** listens to **127.0.0.1:8081** by default. If **`HACKME_COORDINATOR_ADMIN_TOKEN`** is given, the mutating **`POST /api/push_work`**, **`POST /api/work/claim`** and **`POST /api/work/submit`** require **`X-Hackme-Admin-Token`** or **`Authorization: Bearer ...`** (same style as the command node). Without a token, these POSTs are accepted from any client that has reached the bind address - for production, set the token, keep bind on loopback/VPN or enable **`HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=1`** (then the process does not start while `HACKME_COORDINATOR_ADMIN_TOKEN` is empty).

When you change your security policy, update this file and **`docs/API.md`**.
