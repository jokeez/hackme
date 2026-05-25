# Official Bitcoin Core links — what HackMe module #1–5 maps to

Repository: **[bitcoin/bitcoin](https://github.com/bitcoin/bitcoin)** branch **`master`** (May 2026).

| # | HackMe guard | What we test (plain language) | Official Core location |
|---|--------------|------------------------------|----------------------|
| 1 | `rust_script_push_bounds_guard` | `OP_PUSHDATA1` (0x4c) + claimed length **> 520** → violation class | [`script.h` L27–28 `MAX_SCRIPT_ELEMENT_SIZE`](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.h#L27-L28) · [`OP_PUSHDATA1` L82](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.h#L82) · [`GetScriptOp` L312+](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.cpp#L312-L365) |
| 2 | `rust_bounds_guard` | Push opcode validity / element size bound (simplified predicate) | [`HasValidOps()` L299–308](https://github.com/bitcoin/bitcoin/blob/master/src/script/script.cpp#L299-L308) (uses `MAX_SCRIPT_ELEMENT_SIZE`) |
| 3 | `rust_overflow_guard` | Output value in allowed money range (simplified) | [`MoneyRange` L27](https://github.com/bitcoin/bitcoin/blob/master/src/consensus/amount.h#L27) · [`CheckTransaction` L32](https://github.com/bitcoin/bitcoin/blob/master/src/consensus/tx_check.cpp#L32) |
| 4 | `rust_state_transition_guard` | Allowed state transition family (simplified; not a line-by-line fork) | [`CheckTransaction` in validation L795–796](https://github.com/bitcoin/bitcoin/blob/master/src/validation.cpp#L795-L796) · [`AcceptToMemoryPool` L1774](https://github.com/bitcoin/bitcoin/blob/master/src/validation.cpp#L1774) |
| 5 | `cpp_script_push_bounds_guard` | Same push rule as #1, C++ guard twin | [`EvalScript` push size check L447–448](https://github.com/bitcoin/bitcoin/blob/master/src/script/interpreter.cpp#L447-L448) (`SCRIPT_ERR_PUSH_SIZE`) |

## HackMe reproduction sources (our repo)

| Guard | Source |
|-------|--------|
| #1 | [`tasks/sources/security/rust_script_push_bounds_guard.rs`](https://github.com/jokeez/hackme/blob/main/tasks/sources/security/rust_script_push_bounds_guard.rs) |
| #2 | [`tasks/sources/security/rust_bounds_guard.rs`](https://github.com/jokeez/hackme/blob/main/tasks/sources/security/rust_bounds_guard.rs) |
| #3 | [`tasks/sources/security/rust_overflow_guard.rs`](https://github.com/jokeez/hackme/blob/main/tasks/sources/security/rust_overflow_guard.rs) |
| #4 | [`tasks/sources/security/rust_state_transition_guard.rs`](https://github.com/jokeez/hackme/blob/main/tasks/sources/security/rust_state_transition_guard.rs) |
| #5 | [`tasks/sources/security/cpp_script_push_bounds_guard.cpp`](https://github.com/jokeez/hackme/blob/main/tasks/sources/security/cpp_script_push_bounds_guard.cpp) |

## Interpreter reference (consensus enforcement)

When a push exceeds 520 bytes during execution, Core returns **`SCRIPT_ERR_PUSH_SIZE`**:

- [`interpreter.cpp` L447–448](https://github.com/bitcoin/bitcoin/blob/master/src/script/interpreter.cpp#L447-L448)

## Disclaimer

HackMe uses **WASM `check(i64)→i32` gates** inspired by these locations — **not** a fork of the full C++ tree. No substitute for [Bitcoin Core OSS-Fuzz](https://github.com/bitcoin/bitcoin/tree/master/src/test/fuzz) or responsible disclosure to security@bitcoincore.org.
