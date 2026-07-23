# HackMe - development master plan

> **Historical (2025).** Superseded by **[PRODUCTION_MASTER_ROADMAP.md](PRODUCTION_MASTER_ROADMAP.md)** and **[../STATUS.md](../STATUS.md)**. Kept for early MVP context only.

The document brings together the initial plan (“blockchain skeleton → ledger → WASM”), the current state of the code, and the **gaps** that need to be closed for a mature system. The plan file in Cursor is not a replacement: it is a living roadmap **in the repository**.

---

## 1. Vision and boundaries of MVP

**Product Goal:** a local “command node” with a real blockchain on disk, a genesis reward and a demonstration of **Proof-of-Hack** as finding a solution in a **WASM** castle (do not break other people's systems - only synthetic problems in the sandbox).

**Outside the scope of MVP:** mainnet, P2P synchronization, ZK proofs, exchange listings, B2B audit orders.

---

## 2. Catalog map (staffing)

```
HackMe/
├── README.md                 # entry point
├── go.mod / go.sum
├── main.go                   # HTTP, wiring, routes
├── dashboard.html            # UI (embed)
├── metrics.go                # /api/metrics (gopsutil + nvidia-smi)
│
├── docs/                     # human documentation
│   ├── MASTER_PLAN.md        # this file
│   ├── ARCHITECTURE.md       # architecture and flows
│   ├── API.md                # HTTP contract
│   └── SECURITY.md           # MVP threat model, admin token, network checklist
│
├── spec/                     # normative specs (formats, rules)
│   └── CHAIN_SPEC.md
│
├── internal/                 # application code (not imported from outside)
│   ├── block/                # block structure, SHA-256, genesis
│   ├── chain/                # chain service, wallet, miner
│   ├── store/                # SQLite, migrations
│   └── sandbox/              # WASM eval (wazero)
│
├── data/                     # runtime: hackme.db (optionally untracked)
├── tools/                    # stub: build scripts, WASM codegen
├── scripts/                  # stub: deploy, DB backup
└── testdata/                 # stub: binary .wasm for future tests
```

**Rule:** put everything new in the domain in `internal/<domain>`; “what and why” - in `docs/` and `spec/`.

---

## 3. Initial plan - execution status

### Phase 1 - Blockchain Skeleton (Data + Genesis + Button)

| Requirement | Status | Where in the code |
|--------------|--------|------------|
| `Task`, `Block`, `Index`, `Timestamp`, `Hash`, `PrevHash`, `Nonce`, `MinerAddress` | **Done** | `internal/block/types.go` |
| Canonical serialization + SHA-256 | **Done** | `internal/block/hash.go` |
| Genesis, `PrevHash` = 64 zeros, reward 0 HMC (production policy) | **Done** | `internal/block/genesis.go` |
| `POST /api/genesis`, repeat → 409 | **Done** | `main.go`, `internal/chain/service.go` |
| Server log with hash + UI | **Done** | `main.go`, `dashboard.html` |

**Addition to the plan (further recommended):** block diagram version (`schema_version`), explicit field `reward` in the block for emission audit.

### Phase 2 - Local Storage

| Requirement | Status | Where in the code |
|--------------|--------|------------|
| SQLite without CGO | **Done** | `modernc.org/sqlite`, `internal/store/sqlite.go` |
| Tables blocks/meta/wallet | **Done** | migration to `store.Open` |
| `GET /api/wallet`, `GET /api/chain` | **Done** | `main.go` |
| Loading state after restart | **Done** | UI: `refreshWallet` / `refreshStatus` |

**Spaces:** database backup, chain export to file. ~~`PRAGMA user_version`~~ — `internal/store.CurrentSchemaVersion` + bump in `migrate()`; seen in `GET /api/status` as `schema_version` / `schema_expected`.

### Phase 3 - WASM sandbox and mining

| Requirement | Status | Where in the code |
|--------------|--------|------------|
| wazero, minimal module | **Done** | `internal/sandbox/eval.go` (hex module `eval`) |
| Search worker, log of attempts | **Done** | `internal/chain/miner.go` |
| UI Mining + logs (SSE + rollback to polling) | **Done** | `dashboard.html`, `GET /api/mining/logs/stream`, `GET /api/mining/logs` |

**Gaps regarding the “ideal” plan:**

- **SSE** only for **mining logs**; telemetry and graphs are still **polling** `GET /api/metrics`.
- No **`RunLock(input []byte)`** with timeout and fuel limit - now only `Eval(nonce uint64)`; next step: wrapper `context.WithTimeout` + step counter/ fuel API wazero.
- WASM is hardcoded **hex** in the code, not `testdata/*.wasm` - it’s more convenient for the team to put the artifact in `testdata/` and `//go:embed`.

---

## 4. Chain ID and network naming

Constant: **`hackme-dev-mainnet`** (`internal/block/genesis.go`). Displayed in the dashboard header.

**Recommendation:** for a public testnet, later create `hackme-testnet-1` and put it in the config (`HACKME_CHAIN_ID`).

---

## 5. Extended backlog (what was missing in the short plan)

Top to bottom priority - customizable but logical order.

### A. Node Integrity and Security

- Block/transaction signing (Ed25519 or ECDSA), separate `internal/wallet`.
- Checking the chain at start (rehash from genesis to tip).
- Limits on JSON block size, rate limit on API.

### B. Consensus and Network (after one node has stabilized)

- P2P gossip (libp2p or custom UDP/TCP), general `internal/net`.
- Synchronization: request blocks by height / hash.

### C. Proof-of-Hack “in an adult way”

- Tasks like **WASM + manifest** (memory limits, timeout, ABI version).
- Verification of the solution by all nodes equally (determinism).
- Optional: **ZK** only after fixing the language of the statements (what exactly we are proving).

### D. Product and Operations

- YAML/ENV config (`internal/config`).
- Structured logs (slog), levels.
- Prometheus Metrics with `/metrics`.

### E. Legal and ethics (for real customers)

- Only code with **consent** of the owner; technical threat model - `docs/SECURITY.md`; separate legal policy - if necessary, outside the repo.
- Do not position the network as a tool for hacking third parties.

### F. Quality

- `go test ./...` in CI, `golangci-lint`.
- E2E: raise the server, `POST /api/genesis`, check the database.

---

## 6. Next concrete steps (iteration 2–3)

1. Carry out **WASM timeout** and **limit** in `internal/sandbox` + freezing test.
2. Add **`testdata/lock.wasm`** + `go:embed`, remove duplicate hex (or codegen in `tools/`).
3. ~~**Block #1+ with successful PoH**~~ - `chain.Service.AppendPoHBlock`. ~~**Dynamic `poh_target_mod`**~~ - `internal/chain/retarget.go`. ~~**Manifestos `./tasks` + `TaskProvider`**~~ — `internal/chain/taskprovider.go`. ~~**Pool preparation (mock + `push_work` + UI Hive)**~~ - `pool.go`, `dashboard.html`, `/api/network/stats`.
4. **LAN coordinator** (or the first P2P gossip): replacing the mock in `/api/network/stats`, issuing work to workers.
5. ~~**SSE** for mining logs~~ - `GET /api/mining/logs/stream`. Next: SSE/WebSocket for metrics or leave polling.
6. ~~**`PRAGMA user_version`**~~ - see `internal/store/sqlite.go` (`CurrentSchemaVersion`); when changing the circuit - a new step in `migrate()` and find constants.

---

## 7. Risks (briefly)

| Risk | Mitigation |
|------|-----------|
| Dual Genesis | UNIQUE `block_index`, 409 API |
| WASM DoS | timeout `context`, `RuntimeConfig.WithMemoryLimitPages` on sandbox runtimes (see `internal/sandbox`) |
| Loss `data/hackme.db` | backup in `scripts/`, document |
| Regulatory risks of the token | non-ICO, transparent code, utility focus in the docks |

---

## 8. Connection with artifacts

- HTTP details → [API.md](API.md)
- Modules and diagrams → [ARCHITECTURE.md](ARCHITECTURE.md)
- Block byte level and hash → [../spec/CHAIN_SPEC.md](../spec/CHAIN_SPEC.md)

*Update this file when phases change or new modules become available.*
