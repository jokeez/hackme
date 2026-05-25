# Developers & fuzzing orders (localhost model)

## Summary

| Need | Where |
|------|--------|
| Pay for useful-PoW / fuzz orders | **Local node** `http://127.0.0.1:8080/#orders` + developer or admin token |
| Wallet / escrow | **Local node** `#wallet` (your `hackme.db`) |
| Automation | **hackme-fuzzing CLI** → `--base http://127.0.0.1:8080` |
| Watch network | [fuzzing-console.html](https://hackme.tech/fuzzing-console.html) (read-only) |
| Downloads | [downloads.html#local-node](https://hackme.tech/downloads.html#local-node) |

There is **no** order-creation UI on hackme.tech (removed `/pool/developer`).

## Quick start

1. Install/run `hackme-node` (desktop `.env` or VPS-style layout on your PC).
2. Open dashboard → **Developer token** → **Issue** (or `hackme-fuzzing register --base http://127.0.0.1:8080 --save`).
3. **Market** → upload `.wasm` → POST order (debits local wallet).
4. When an order is open, the public pool runs in **orders** mode: workers get `wasm_check_hex` in `/api/work/claim`, pass the gate on a PoH hit, and relay **`POST /api/poh/solve-order`** — escrow credits the solver’s `miner_address`. See `docs/ORDER_ECONOMICS.md`.

## Auth

| Token | Scope |
|-------|--------|
| **Developer** | `GET/POST /api/tasks` on the node that issued it |
| **Admin** | Full node incl. `from_code`, fuzz campaigns |

Register on the **same node** where you create orders:

```bash
export HACKME_FUZZING_BASE=http://127.0.0.1:8080
hackme-fuzzing register --save
hackme-fuzzing build -lang rust -source check.rs -out ./fuzzing-out
hackme-fuzzing create ./fuzzing-out/my-order.manifest.json
```

## Public site limits (by design)

- `POST /api/tasks` on hackme.tech — only with developer token (integrator testing); **customers should use localhost**.
- `POST /api/tasks/from_code` — **403** on hackme.tech.
- `/api/fuzz/*` admin — **403**; report paths gated by report token.

## Related

- [PORTALS_FINAL_VERDICT.md](PORTALS_FINAL_VERDICT.md)
- [FUZZING_B2B_SECURITY_VERDICT.md](FUZZING_B2B_SECURITY_VERDICT.md)
