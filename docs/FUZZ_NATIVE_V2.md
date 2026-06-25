# Fuzz native bridge v2 — Bitcoin script guards

## What changed

Native repro (`internal/fuzznative`) now mirrors **all 10 Bitcoin30 WASM guards**, not only dup-inputs / BIP34 / block-weight.

| Guard | Native eval |
|-------|-------------|
| `bitcoin_tx_dup_inputs` | dup prevout keys |
| `bitcoin_coinbase_bip34` | height push |
| `bitcoin_block_weight` | 4M WU |
| `bitcoin_getscriptop` | GetScriptOp + 520B |
| `bitcoin_hasvalidops` | HasValidOps walk |
| `bitcoin_evalscript_push` | SCRIPT_ERR_PUSH_SIZE |
| `bitcoin_witness_stack` | witness element sizes |
| `bitcoin_evalscript_stack` | stack+altstack 1000 |
| `bitcoin_evalscript_opcount` | 201 op budget |
| `bitcoin_tx_check` | MoneyRange (int32 outputs) |

## Example (before → after)

**Day 18 · evalscript_push · wasm_native**

| | Before v2 | After v2 |
|---|-----------|----------|
| WASM guard signals | 80 | 80 |
| `native_confirmed` | **0** (generic dup port) | **matches WASM** on pinned script logic |
| Bounty gate | blocked | eligible when native confirms |

**OSS hunts** (`run_oss_pr_fuzz_hunt.sh`) default to `wasm_native` + `guard_name` + native repro enabled.

## Honest scope

Still Go ports of `tasks/sources/security/upstream/*.c` — not `bitcoind` binary fuzz. Next step: compile upstream C harness per target for Tier-C disclosure.

## Tests

```bash
go test ./internal/fuzznative/... -count=1
bash scripts/ops/fuzz_depth_v3_gate.sh
```
