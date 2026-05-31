# Upstream excerpts (L1 stack guards)

Portable C excerpts compiled to WASM (`check(i64)->i32`). Each module cites the source file and function in Bitcoin Core, go-ethereum, Dogecoin, Litecoin, or HackMe.

| WASM artifact | Upstream | Function / rule |
|---------------|----------|-------------------|
| `upstream_bitcoin_getscriptop.wasm` | [bitcoin/bitcoin](https://github.com/bitcoin/bitcoin) `src/script/script.cpp` | `GetScriptOp` + `MAX_SCRIPT_ELEMENT_SIZE` (520) |
| `upstream_bitcoin_hasvalidops.wasm` | bitcoin/bitcoin `script.cpp` | `CScript::HasValidOps()` |
| `upstream_bitcoin_tx_check.wasm` | bitcoin/bitcoin `consensus/tx_check.cpp` + `amount.h` | `CheckTransaction` output loop + `MoneyRange` |
| `upstream_ethereum_value_overflow.wasm` | [ethereum/go-ethereum](https://github.com/ethereum/go-ethereum) `core/state_transition.go` | `uint256.FromBig` overflow on tx value (128-bit slice) |
| `upstream_dogecoin_hasvalidops.wasm` | [dogecoin/dogecoin](https://github.com/dogecoin/dogecoin) `script.cpp` | Same family as Bitcoin `HasValidOps` |
| `upstream_litecoin_getscriptop.wasm` | [litecoin-project/litecoin](https://github.com/litecoin-project/litecoin) `script.cpp` | `GetScriptOp` + 520 B push cap |
| `upstream_hackme_order_gate.wasm` | [jokeez/hackme](https://github.com/jokeez/hackme) `internal/chain/order_tasks.go` | `InsertOrderTask` manifest bounds |

Input encoding (all modules): low 64 bits of `i64` carry probe payload (script bytes LE, output sats, manifest fields). See `tools/l1stack_report/probe.go`.

Not a substitute for running full `bitcoind` / `geth` — reproducible fuzz surface on HackMe sandbox only.
