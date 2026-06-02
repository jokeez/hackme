# HMS storage market — kernel-locked pricing

Rates live in `internal/hms/pricing.go` as **compile-time constants** (not env-tunable).
Changing them requires a coordinated upgrade and a new `market_policy_hash`.

## Rates (aligned with HMC order kernel)

| Parameter | Value | Chain twin |
|-----------|-------|------------|
| Storage | **0.002 HMC / GB·month** | — |
| Platform fee | **5%** of storage subtotal | `OrderPlatformFeeRate` |
| Burn tally | **10%** of storage subtotal | `OrderBurnRate` |
| Min prepaid | **0.01 HMC** | — |
| Min billable | **5 GB** per order (floor) | — |
| Retention | **7–365 days** (default 30) | — |

## Payment flow

1. `POST /api/hms/market/quote` — kernel quote + `quote_hash`
2. `POST /api/hms/market/orders` on **desktop node** — debits HMC wallet (`PayHMSStorageMarket`), forwards to coordinator with `payment_id`
3. Upload encrypted chunks with `X-HMS-Upload-Token`
4. `POST …/complete` — order `stored`
5. Restore: `GET …/chunks` + `GET …/download/{index}` + client decrypt
6. **Restore pack**: export `HMSRESTORE1:` base64 bundle (upload token + file manifest) — import on another device; passphrase stays local.

Production:
- `HMS_MARKET_REPLICAS` (default **2**) — chunk copies on distinct storage workers
- Market upload **24/7** — not blocked by seal epoch freeze (`ord-*` chunks excluded from seal manifest)

Pilot: coordinator may set `HMS_MARKET_SKIP_PAYMENT=1` (localhost only).

## Verify

```bash
go test ./internal/hms/ -run Market
go test ./internal/chain/ -run HMSMarket
bash scripts/check_hms_market_invariants.sh
bash scripts/tests/hms_full_gate.sh
```
