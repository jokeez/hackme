# Rust/C++ Task Quickstart

This quickstart prepares cross-language WASM gates for Orders.

## Prerequisites (Ubuntu)

```bash
sudo apt update
sudo apt install -y clang lld
curl https://sh.rustup.rs -sSf | sh -s -- -y
source "$HOME/.cargo/env"
rustup target add wasm32-unknown-unknown
```

## Build artifacts + manifests

From repository root:

```bash
bash scripts/build_task_wasm.sh
```

This creates:

- `tasks/artifacts/rust_check.wasm`
- `tasks/artifacts/cpp_check.wasm`
- `tasks/manifests/order-rust-001.json`
- `tasks/manifests/order-cpp-001.json`

The script also runs ABI validation (`check(i64)->i32`) via:

```bash
go run ./tools/task_abi_check tasks/artifacts/rust_check.wasm tasks/artifacts/cpp_check.wasm
```

and manifest linting via:

```bash
go run ./tools/task_manifest_lint tasks/manifests/order-rust-001.json tasks/manifests/order-cpp-001.json
```

## Submit order manifests

Using API:

```bash
curl -s -X POST http://127.0.0.1:8080/api/tasks \
  -H "Content-Type: application/json" \
  --data-binary @tasks/manifests/order-rust-001.json | jq

curl -s -X POST http://127.0.0.1:8080/api/tasks \
  -H "Content-Type: application/json" \
  --data-binary @tasks/manifests/order-cpp-001.json | jq
```

Or copy JSON files into Orders UI and run `POST /api/tasks`.

## Verify

- `GET /api/tasks` shows both orders.
- Status transitions `open -> completed`.
- `progress_count` reaches `target_solves`.
