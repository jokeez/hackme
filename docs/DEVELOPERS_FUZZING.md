# Developers & fuzzing orders (localhost model)

## Summary

| Need | Where |
|------|--------|
| Pay for useful-PoW / fuzz orders | **Local node** `http://127.0.0.1:8080/#orders` + developer or admin token |
| Wallet / escrow | **Local node** `#wallet` (your `hackme.db`) |
| Automation | **hackme-fuzzing CLI** → `--base http://127.0.0.1:8080` only (wizard **blocks** hackme.tech) |
| Watch network | [fuzzing-console.html](https://hackme.tech/fuzzing-console.html) (read-only) |
| Product guide | [fuzz-guide.html](https://hackme.tech/fuzz-guide.html) · [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) |
| Hunt (ASAN repo) | [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) · CLI `hackme-fuzzing hunt` · [API.md](API.md)#hunt-campaigns-phase-2 |
| Downloads | [downloads.html#local-node](https://hackme.tech/downloads.html#local-node) |

There is **no** order-creation UI on hackme.tech (removed `/pool/developer`).

---

## Quick start

1. Install/run `hackme-node` (desktop `.env` or VPS-style layout on your PC) on `http://127.0.0.1:8080`.
2. Ensure **local spendable HMC** (`GET /api/wallet` → `balance_orders_spendable_hmc`). On-chain display alone is not enough for escrow. Fresh installs get a local wallet via genesis; follower desktops must fund the local wallet row.
3. Open dashboard → **Developer token** → **Issue**, or (when `HACKME_INTEGRATOR_SELF_REGISTER=1`):

```bash
hackme-fuzzing register --base http://127.0.0.1:8080 --save
```

4. **Recommended (B2B final):**

```bash
export HACKME_ADMIN_TOKEN=…   # or use saved developer token
hackme-fuzzing wizard --pack secrets --package audit --title "Secrets scan"
hackme-fuzzing wizard --pack filter_utf8 --package audit --title "FluxTap-class filter preflight"
hackme-fuzzing wizard --wasm ./guard.wasm --package deep --public-proof
```

5. Wizard prints `report_url`, `gate_url`, `pulse_url` on **your node** (`http://127.0.0.1:8080/...`), plus one-time `customer_report_token`. Override host only with `HACKME_PUBLIC_REPORT_BASE` if you mirror reports.
6. Pool miners pick up `pool_distributed` campaigns; hybrid rigs run fuzz in PoH backpressure windows.

### Hunt (separate from Dig wizard)

```bash
export HACKME_ADMIN_TOKEN=…
hackme-fuzzing hunt packages
hackme-fuzzing hunt inventory --path ./my-repo
# then template / build / create — see HUNT_ECONOMICS.md
hackme-fuzzing hunt create --package hunt_lite   # or hunt_standard | hunt_heavy
```

Hunt uses **50/50** escrow (not Dig 20/80). Packages: Lite ~20 HMC · Standard ~60 · Heavy ~150.

---

## Packages

### Dig (WASM · `wizard`)

| Package | HMC | Runs | exec/unit | Pool |
|---------|-----|------|-----------|------|
| **scan** | ~1 | 64 | 1 | local |
| **audit** | ~5 | 256 | 64 | yes |
| **deep** | ~25 | 2048 | 512 local · **hub cap 256** (`HACKME_POOL_EXEC_PER_UNIT_CAP`) | yes |

**Packs:** `secrets` · `script_bounds` · `filter_utf8` · `parser_expat` — `hackme-fuzzing packs`

### Hunt (ASAN · `hunt` CLI)

| Package | HMC | Shards | exec/shard | Escrow |
|---------|-----|--------|------------|--------|
| **hunt_lite** | ~20 | 1200 | 32 | 50/50 |
| **hunt_standard** | ~60 | 4000 | 128 | 50/50 |
| **hunt_heavy** | ~150 | 12000 | 256 | 50/50 |

Full economics: [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md).

---

## Coverage (do not oversell)

- **`wasm_edge_bitmap`** on instrumented detector packs — 256-byte bitmap at WASM memory offset **8192** for guided scheduling. **Not** AFL/libFuzzer edge coverage.
- **`input_fingerprint`** on parser packs; `parser_expat` adds native ASAN repro on pinned upstream.

---

## Auth

| Token | Scope |
|-------|--------|
| **Developer** | `GET/POST /api/tasks` on the node that issued it |
| **Admin** | Full node incl. `from_code`, fuzz campaigns, `POST /api/security-audit` |

```bash
export HACKME_FUZZING_BASE=http://127.0.0.1:8080
hackme-fuzzing register --save
hackme-fuzzing build -lang rust -source check.rs -out ./fuzzing-out
hackme-fuzzing create ./fuzzing-out/my-order.manifest.json

# CI gate (primary deliverable):
curl -sS -H "X-Hackme-Report-Token: $TOKEN" \
  "http://127.0.0.1:8080/api/fuzz/campaigns/$CAMPAIGN_ID/gate?max_critical=0&max_high=0"

hackme-fuzzing status --campaign "$CAMPAIGN_ID" --report-token "$TOKEN"
```

Classic manifest path remains for integrators; **wizard + packs** is the supported product layer.

---

## Public site limits (by design)

- `POST /api/tasks` on hackme.tech — only with developer token (integrator testing); **customers use localhost**.
- `POST /api/tasks/from_code` — **403** on hackme.tech.
- `/api/fuzz/*` admin — **403** on hub; report paths gated by report token.

---

## Real-project example: FluxTap-class filter

Pack **`filter_utf8`** ships seeds including `\xc7=` — the same malformed expression class that panics in [FluxTap](https://github.com/FounderB/FluxTap) `filter.go` (invalid UTF-8 + `ToLower` index skew). Run locally before shipping a display-filter parser:

```bash
hackme-fuzzing wizard --pack filter_utf8 --package audit --title "Filter parser preflight"
```

Repo test: `go test ./tools/fluxtap_wasm_compare/ -v`

---

## Related

- [FUZZING_B2B_SECURITY_VERDICT.md](FUZZING_B2B_SECURITY_VERDICT.md)
- [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md)
- [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md)
- [CUSTOMER_FUZZ_DELIVERABLES.md](CUSTOMER_FUZZ_DELIVERABLES.md)
