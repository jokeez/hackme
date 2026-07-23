# Preparing the wallet and network for listing on exchanges (technical contract)

This document is **technical** preparation: address format, translations, units, API for integration. **Not** legal and **not** a guarantee of acceptance by the exchange: each platform has its own onboarding (project KYC, AML, liquidity volume, code and audit requirements).

## 1. Network identifiers (what to send to the integration form)

| Field | Meaning in current code | Comment |
|------|-------------------------|-------------|
| **Boolean chain id** | `hackme-dev-mainnet` (constant, see `spec/CHAIN_SPEC.md`, `GET /api/status` → `chain_id`) | A **product listing** typically requires a **stable** public ID and **one** consensus chain; change `chain_id` = essentially a new network for the exchange. |
| **Ticker (suggested)** | `HMC` (by address prefix and documentation) | Final approval with the exchange (collisions of tickers on the market). |
| **Minimum Unit** | **Kapa** | `1 HMC = 100_000_000` Kapa (`uint64` in API). **No floats** in the wire protocol. |
| **Address** | String `HMC-` + **16 hex characters** (lower case), total **20 characters** prefixed | Deterministically from Ed25519 pubkey: `SHA256(pubkey)[:8]` → 16 hex (see `cmd/minersign`, `tools/scan_ed25519_seeds`). |
| **Translation Signature** | Ed25519, `transfer_v1` | Fields and order of keys for signature - **`spec/CHAIN_SPEC.md`** section “Translations”. |
| **Minimum commission** | `1000` Kapa (`DefaultTransferMinFee` to `internal/chain/transfers.go`) | Less - tx will deviate (`invalid_fee`). |
| **Commission Distribution** | 30% burn / 70% on consensus `DevFeeAddress` | See `docs/API.md` → `POST /api/tx/send`; without runtime-override. |
| **Memo/tag** | Optional, up to **256 bytes** UTF-8 | If the exchange issues a **deposit memo/tag**, it must be placed in **`memo`** in the body `transfer_v1` (with support on the wallet/script side). |

## 2. HTTP API for integration (read + write)

Base URL command node (public): for example `https://hackme.tech` (the specific host is with the operator).

| Destination | Method | Path |
|------------|--------|------|
| Height, Economy, `policy_hash` | GET | `/api/status` |
| Balance and `next_nonce` | GET | `/api/address/{address}` |
| Sending a transfer | POST | `/api/tx/send` |
| tx status | GET | `/api/tx/{hash}` |
| Mempool (operator) | GET | `/api/tx/pool` |
| Blocks (browser) | GET | `/api/chain?limit=…`, `/api/reports/blocks?…` |

Regulatory translation description: **`spec/CHAIN_SPEC.md`**, HTTP details: **`docs/API.md`**.

## 3. What exchanges often ask for (compare with their PDF yourself)

- **Fixed decimals** - you have **8** (Kapa per HMC).
- **Finality / confirmations** - in MVP it is usually considered **inclusion in a block**; record the explicit number “N of confirmations” with the exchange and with `GET /api/tx/{hash}`.
- **No reorg/arb policy** - see `consensus_policy` in `/api/status` (`no_reorg_v1_fail_closed` etc.) - attach JSON to exchange.
- **Genesis, max supply, policy hash** — fields `economics` and `policy_hash` in `/api/status`.
- **Source code/reproducible build** - repository + `go version`, optional `reports/pool-freeze-*` from `scripts/ops/pool_release_freeze.sh`.
- **Test network** - if the exchange requires a **separate testnet**: raise a separate command node + **new** DB and **obviously different** `chain_id` (will require code/constant changes and approval - do not mix with prod).

## 4. Preparation of wallets (operational minimum)

1. **Separate “exchange” wallet**  
   - Do not use the same seed as the node/treasury in an open environment.  
   - Generate a key: `go run ./cmd/minersign -gen-seed` → save **seed / pubkey / HMC address** in vault (password manager, HSM - if possible).

2. **Address verification**  
   - `GET /api/address/<HMC-...>` on the canonical node should return the expected `balance_units` / `next_nonce` after test replenishment.

3. **Test deposit cycle**  
   - Small amount: `POST /api/tx/send` with correct `amount_units`, `fee_units >= 1000`, correct `nonce`.  
   - Make sure that the exchange sees the tx using the **same** `tx_hash` that the API returns (hex from the response).

4. **Memo**  
- If the exchange gave **destination tag**: include in `memo` when generating JSON for signature (and do not trim UTF-8 beyond the limit).

5. **Reserve and recovery**  
   - Offline copy of seed; procedure in case of compromise - **new** key and new deposit address at the exchange.

## 5. What to send to the exchange integration team (template)

You can insert it into a letter as an attachment:

```
Network name: HackMe (self-hosted command node)
Chain ID (logical): hackme-dev-mainnet   [or your production id after agreement]
Address format: HMC-<16 lowercase hex>  (example: HMC-0123456789abcdef)
Decimals: 8 (1 HMC = 100_000_000 Kapa)
Signature: Ed25519, tx_type transfer_v1 (see attached CHAIN_SPEC.md § transfers)
Min fee: 1000 Kapa
RPC-style HTTP base: https://<your-command-node>/
Endpoints: GET /api/status, GET /api/address/{addr}, POST /api/tx/send, GET /api/tx/{hash}
Block explorer: https://<your-command-node>/explorer  [if enabled]
Policy / economics: GET /api/status → economics + policy_hash
```

Attach files from the repository: **`spec/CHAIN_SPEC.md`**, **`docs/API.md`** (transfers section).

## 6. What is not in the code **yet** (do not promise to the exchange without modification)

- Official **SLIP-0044** coin type for HD wallets.  
- Standardized **gRPC** instead of JSON HTTP.  
- **Hardware wallet** (Ledger, etc.) is a separate project.  
- Automatic **bridge** to other networks.

Bottom line: **preparing for listing** with the current stack can be done as **HTTP integration + fixed wire format + separate keys and test translations**; “everything according to the rules of the exchanges” in the legal sense goes **only** through their checklists and project lawyers.
