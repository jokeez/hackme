# Checkpoint Report — 2026-04-20

## Scope

Private-testnet validation of:

- transfer flow (happy + negative)
- order lifecycle and fairness guard
- Rust/C++ WASM task pipeline
- synthetic security task pack execution

## Key Results

### 1) Transfer flow

- Happy path transfer reached `included` with valid `block_index`/`block_hash`.
- Balance/fee/nonce math matched expectations.
- Negative cases returned expected codes:
  - `invalid_nonce`
  - `invalid_signature`
  - `insufficient_balance`

### 2) Test suite

- `go test ./...` passed.
- Critical domains validated by tests: `internal/chain`, `cmd/coordinator`, `internal/sandbox`, `internal/store`, `internal/lanpool`, `internal/nodecrypto`.

### 3) Orders and fairness

- Fairness guard rejected low-reward manifest at `difficulty_score=70` with expected minimum reward validation.
- Existing order scenarios (`light/medium/heavy`) reached completion in UI/API.

### 4) Rust/C++ baseline WASM orders

- `order-cpp-001` completed (`3/3`).
- `order-rust-001` completed (`3/3`) after optimizing Rust wasm size and hash update.

### 5) Security task pack (Rust + C++)

All six orders completed (`3/3` each):

- `order-cpp-bounds_guard-001`
- `order-cpp-overflow_guard-001`
- `order-cpp-state_transition_guard-001`
- `order-rust-bounds_guard-001`
- `order-rust-overflow_guard-001`
- `order-rust-state_transition_guard-001`

Verification command used:

```bash
curl -s http://127.0.0.1:8080/api/tasks \
| jq -r '.tasks[]
| select(.id | test("order-(rust|cpp)-(bounds_guard|overflow_guard|state_transition_guard)-001"))
| "\(.id) \(.status) \(.progress_count)/\(.target_solves)"'
```

Observed final state: all six lines reported `completed 3/3`.

## Conclusion

Current node/coordinator/order pipeline is in a strong state for private-stage operation:

- correctness checks are green (manual + automated)
- economic and validation guards behaved as designed
- cross-language WASM task path is proven end-to-end

## Next Main Risk Block

Proceed to **multi-PC coordinator stress**:

- multi-worker claim/submit races
- lease expiry/reissue behavior
- duplicate result hash rejection
- consistency of `work/stats` counters under sustained load
