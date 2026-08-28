# Fuzz campaigns — product & operator guide

HackMe **B2B security fuzz** runs on your **local node** (`127.0.0.1:8080`). The public site is catalog + onboarding only — orders, wallet, and reports stay on your machine.

**Customer entry:** `hackme-fuzzing wizard` · **Site guide:** [fuzz-guide.html](https://hackme.tech/fuzz-guide.html) · **HTTP tables:** [API.md](API.md) (Fuzz Campaigns)

---

## 1) Packages (Scan · Audit · Deep)

| Package | HMC | Runs | exec/unit (local) | Pool | Best for |
|---------|-----|------|-------------------|------|----------|
| **scan** | ~1 | 64 | 1 | local only | CI smoke, nightly guard |
| **audit** | ~5 | 256 | 64 | yes | Protocol guards, DeFi invariants |
| **deep** | ~25 | 2048 | 512 | yes* | Byte corpus, hours-scale campaign |

\* **Distributed pool cap:** on hub workers, `exec_per_unit` is capped at **64** per work item (Deep 512 runs locally on autorunner). Coordinator **replays** the segment on submit — miners do not cryptographically attest every exec.

```bash
export HACKME_ADMIN_TOKEN=…   # local node
hackme-fuzzing wizard --pack filter_utf8 --package audit --title "FluxTap preflight"
hackme-fuzzing wizard --wasm ./guard.wasm --package deep --public-proof
hackme-fuzzing packs   # list ready packs
```

Wizard prints: `campaign_id`, `customer_report_token`, `report_url`, `gate_url`, optional `proof_url`.

---

## 2) Ready packs (detectors)

| Pack | What it catches |
|------|-----------------|
| **secrets** | AWS/API key patterns in byte inputs |
| **script_bounds** | Bitcoin-class script push bound violations |
| **filter_utf8** | Invalid UTF-8 + operator index skew (FluxTap-class display filter panic on `\xc7=`) |
| **parser_expat** | XML byte corpus; native ASAN on pinned libexpat (`native_repro_mode: oss_upstream`) |

Pack-aware budgets override generic package defaults (see `hackme-fuzzing packs --json`).

---

## 3) Coverage semantics (honest)

| `coverage_kind` | Where | Meaning |
|-----------------|-------|---------|
| **wasm_edge_bitmap** | `secrets`, `filter_utf8`, `script_bounds` | Instrumented guard writes a **256-byte edge counter bitmap** at linear memory offset **8192** after each `check_bytes` — used for **guided scheduling**, not AFL path coverage |
| **input_fingerprint** | `parser_expat`, legacy guards | Hash/fingerprint buckets only |

OSS research (libFuzzer on nghttp2/expat) is a **separate lane** — do not conflate with customer `wasm_edge_bitmap`.

---

## 4) Guard ABI

- Legacy: `check(i64) -> i32`
- Bytes mode: `check_bytes(ptr, len) -> i32` with `input_mode=bytes`
- Template: `tasks/sources/security/rust_customer_bytes_guard_template.rs`

---

## 5) Deliverables

1. **HTML report** — `GET /api/fuzz/campaigns/{id}/report` + `X-Hackme-Report-Token`
2. **CI gate** — `GET /api/fuzz/campaigns/{id}/gate?max_critical=0&max_high=0`
3. **Pulse** — live progress for dashboards
4. **Proof of Fuzz** (opt-in) — `wizard --public-proof` → `/proof/{id}` + badge (crash-gate only; secrets redacted)

See [CUSTOMER_FUZZ_DELIVERABLES.md](CUSTOMER_FUZZ_DELIVERABLES.md).

---

## 6) Pool distributed fuzz

When `pool_distributed: true`, hub `workerfuzz` / hybrid `workerpoh` claims work via `/api/fuzz/work/claim`.

**Anticheat (rc16 / Phase 2):**

- Guided campaigns freeze `corpus_seeds` + anchor input at **claim**
- Submit requires matching `segment_exec_done` and coordinator full-segment replay
- Invalid WASM → reject; incomplete segment → reject
- Worker lease scales with segment wall time (not fixed 30s)
- Submit nonce reserved at signature validate (anti-replay)

**Safe fleet tiers:** Scan (1 exec) and Audit (64 exec). Treat pool Deep as **cap-64** until attestation ships.

Details: [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md).

---

## 7) Hybrid mining (unchanged)

GPU rigs run PoH by default; fuzz fills idle/backpressure slots (`HACKME_WORKER_HYBRID_FUZZ=1`). PoH GH/s is primary; fuzz adds coordinator accrual on completed pool work — not a separate “mining algorithm.”

---

## 8) Release gate (operators)

```bash
ADMIN_TOKEN=… BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_release_gate.sh
bash scripts/ops/fuzz_b2b_final_gate.sh   # B2B wizard + packs smoke
MODE=lang_static bash scripts/tests/run_daily.sh
```

---

## 9) Autorunner & env

| Env | Role |
|-----|------|
| `HACKME_FUZZ_AUTORUN` | Server-side campaign ticking (default on) |
| `HACKME_FUZZ_AUTORUN_TICK_SEC` | Tick interval 1–60s |
| `HACKME_FUZZ_ARTIFACT_DIR` | Artifact root |
| Per-campaign `config.auto_runner=false` | Skip autorunner for one row |

---

## 10) Related docs

- [DEVELOPERS_FUZZING.md](DEVELOPERS_FUZZING.md) — localhost model, auth, CLI
- [FUZZ_ESCROW_20_80.md](FUZZ_ESCROW_20_80.md) — 20/80 split
- [FUZZING_B2B_SECURITY_VERDICT.md](FUZZING_B2B_SECURITY_VERDICT.md) — threat model
