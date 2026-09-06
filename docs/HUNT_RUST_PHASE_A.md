# Hunt Rust — Phase A

Status: **shipped on `feature/hunt-mvp`** (inventory detect + catalog ASAN Rust targets).

## What works

| Layer | Behavior |
|-------|----------|
| **Inventory** | Scans `.rs` for `fuzz_target!` / `libfuzzer_sys` / `LLVMFuzzerTestOneInput`; sets `language=rust`; hints `cargo_present` / `rust_sources` |
| **Catalog** | Optional `"language": "rust"` (+ optional `cargo_package`) on `upstream/oss_cve_targets.json` (default language `c`) |
| **Build** | `BuildTarget` → `cargo +nightly` with `RUSTFLAGS=-Zsanitizer=address` → `.cache/oss-cve-bin/<id>-*.bin` |
| **Soak** | Same `hunt_overnight_soak.sh` / `hunt-bench-local`; preflight accepts `*_stdin.c` **or** `*_stdin.rs`; bench binary is rebuilt each soak |
| **Report** | `HuntReport.language` + catalog `TargetSummary.language` + bench summary `language` |

## Catalog Rust targets (2026-09-06)

| ID | Surface | Why |
|----|---------|-----|
| `serde_json` | Safe JSON parse | Pipeline pilot (expect CLEAN) |
| `memchr` | Unsafe SIMD / memmem | Real unsafe byte-search surface |
| `quick_xml` | XML event parser | Unsafe-heavy parser; bounty-shaped |

Drivers: `tasks/sources/fuzz/oss/{serde_json,memchr,quick_xml}_stdin.rs`.

## Requirements

```bash
rustup toolchain install nightly
# clang still required for C/C++ catalog targets
```

## Commands

```bash
# Gate (unit + ASAN smoke for all three Rust catalog targets)
bash scripts/tests/hunt_inventory_rust_gate.sh

# Build Rust targets
TARGETS=serde_json,memchr,quick_xml bash scripts/ops/build_oss_cve_pack.sh

# Short local Hunt (unsafe-shaped)
WALL_SEC=120 HUNT_ITER=2000 TARGETS=memchr,quick_xml RUN_LIBFUZZER=0 \
  bash scripts/ops/hunt_overnight_soak.sh
```

## Out of Phase A

- Auto-compile arbitrary customer `cargo fuzz` crates via `POST /api/hunt/harness/build` (fails closed with a clear error)
- Pool harness publish for Rust
- C# / SharpFuzz

## Honest note

Safe Rust (`serde_json`) rarely yields ASAN heap CVEs — it proves the **pipeline**. Prefer `memchr` / `quick_xml` (and future `unsafe`/FFI crates) for bounty signal. LSan harness bugs (see sheredom) still apply — free/drop owned allocations in drivers.
