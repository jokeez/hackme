# Hunt Rust — Phase B

Status: **shipped on fork** (`feat/hunt-engine-depth`).

## What Phase B adds (over Phase A)

| Layer | Phase A | Phase B |
|-------|---------|---------|
| Catalog Rust targets | ✅ serde_json, memchr, quick_xml | ✅ unchanged |
| Inventory detect | ✅ fuzz_target! scan | ✅ unchanged |
| **Customer build** | ❌ fails closed | ✅ `POST /api/hunt/harness/build` for `.rs` |
| **cargo-fuzz** | ❌ | ✅ auto when `fuzz/Cargo.toml` present |
| **stdin driver** | catalog only | ✅ extracts `fuzz_target!` body → ASAN stdin binary |
| **Pool publish** | C/C++ harness | ✅ Rust binary via same publish path |

## Build modes

1. **cargo_fuzz** — source under `fuzz/fuzz_targets/` **and** pinned repo has `fuzz/Cargo.toml` → `cargo +nightly fuzz build <target> --sanitizer=address`
2. **stdin_fuzz_target** — `.rs` with `fuzz_target!(|data: &[u8]| { ... })` → stdin ASAN binary (preserves param name + non-libfuzzer `use` imports)
3. **stdin_package** — **fail closed** (no stub driver). Provide a real `fuzz_target!` or cargo-fuzz target.

## Requirements

```bash
rustup toolchain install nightly
cargo install cargo-fuzz   # optional, for cargo_fuzz mode
```

## API

Same as C/C++ inventory:

```http
POST /api/hunt/harness/build
POST /api/hunt/harness/publish
```

## Gate

```bash
bash scripts/tests/hunt_inventory_rust_gate.sh
go test ./internal/hunt/ -run 'Rust|FuzzTarget|PlanRust' -count=1
```

## Out of Phase B

- Arbitrary workspace monorepos without fuzz/ layout
- C# / SharpFuzz
- libFuzzer in-process coverage export to pool (research lane)
