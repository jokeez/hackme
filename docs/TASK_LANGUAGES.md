# Task Languages Policy (WASM ABI v1)

This document specifies which languages should be enabled for **Orders** tasks (`POST /api/tasks/from_code`) and in what order.

> **Not Hunt inventory.** Hunt ASAN languages (C/C++/Rust Phase A catalog) are documented in [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md) and [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) — different product surface from WASM `check(i64)->i32` orders.

## Basic principle

Language support = compilation in WASM with ABI:

- export exactly one: `check`
- signature: `check(i64) -> i32`

If the ABI is not met, the module should not be included in production.

## Language status

| Language | Status | Comment |
|---|---|---|
| Rust | Ready now | The best main language for tasks, stable wasm toolchain (`rustc`). |
| C | Ready now | `language: "c"`, alias `gcc`; `clang` wasm target, like C++ but `-x c`. |
| C++ | Ready now | `language: "cpp"`; `clang` wasm + `-Wl,--export=check`. |
| Assembly (WAT/wasm text) | Ready now (guarded) | `language: "wat"` (aliases: `asm`, `assembly`, `wast`, `wasm-text`), `wat2wasm`. |
| Python | Later via WASM only | Native Python runtime in a node is not recommended; only sandboxed wasm path after strict limits. |
| Go (TinyGo) | Ready now (guarded) | `tinygo` (`go`/`golang`); sanitize + strict ABI gate. |
| Zig | Ready now (guarded) | `zig build-lib` → wasm32-freestanding; sanitize + gate; the node needs `zig` in PATH. |
| AssemblyScript | Ready now (optional toolchain) | `language: "assemblyscript"` or `as`; CLI `asc` (`npm i -g assemblyscript`); sanitize + gate. |

## What not to do

- Do not run arbitrary `py` / `exe` / `sh` from orders.
- Do not add different ABIs for different languages.
- Do not accept artifacts without checking ABI and hash.

## Minimal process for adding a new language

1. Compilation in `.wasm`.
2. ABI check: `go run ./tools/task_abi_check <artifact.wasm>`.
3. SHA-256 counting.
4. Create a manifest (`wasm_artifact_path` + `artifact_hash`).
5. Manifest lint: `go run ./tools/task_manifest_lint <manifest.json>`.
6. Download via `POST /api/tasks`.

## Campaign-mode (fuzz/property/symbolic)

For a language to be useful for a white-hat network, `POST /api/tasks` alone is not enough:

- campaigns are created via `POST /api/fuzz/campaigns` (`campaign_type`: `fuzz|property|symbolic`);
- results/finds are published in batches via `POST /api/fuzz/campaigns/{id}/findings`;
- the customer is given a unified report `GET /api/fuzz/campaigns/{id}/report` (`fuzz_report_v1`).

## Shortcuts

```bash
bash scripts/build_task_wasm.sh
go run ./tools/task_abi_check tasks/artifacts/rust_check.wasm tasks/artifacts/cpp_check.wasm
go run ./tools/task_manifest_lint tasks/manifests/order-rust-001.json tasks/manifests/order-cpp-001.json
```

Full regression on **all** manifests/wasm and live language matrices (with a running node): **`scripts/tests/run_language_production_pack.sh`**.
