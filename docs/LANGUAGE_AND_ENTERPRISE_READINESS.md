# Task languages ​​and “enterprise” readiness

## What is already supported (from_code/toolchain)

One source of truth on the status of languages: **[`docs/TASK_LANGUAGES.md`](TASK_LANGUAGES.md)**  
(Rust, C/C++, Zig, TinyGo, AssemblyScript, WAT, negative cases and prohibition of arbitrary runtimes.)

## How to run existing languages ​​before selling

1. **Static without node** (manifests + ABI of all `.wasm` in `tasks/`):

   ```bash
   STATIC_ONLY=1 bash scripts/tests/run_language_production_pack.sh
   ```

Or through a general runner (the report goes to `reports/tests/<RUN_ID>/` for `report_summary`):

   ```bash
   MODE=lang_static RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)" bash scripts/tests/run_daily.sh
   ```

A static language step is also performed at the beginning of **`MODE=full`** and **`MODE=pre_release`**.

2. **Full language pack** (you need a running node with the same compilers as on the prod-toolchain VPS, plus an admin token):

   ```bash
   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 bash scripts/tests/run_language_production_pack.sh
   ```

3. **Inside the general regression** - already included in `scripts/ops/fuzz_release_gate.sh` and `scripts/ops/run_local_full_matrix.sh` (ephemeral stack).

The order of work is meaningful: **first a green static package → then live matrices on the stand with full PATH** → only then adding new languages ​​or weakening the gate.

## Ladder purlins (briefly)

| Goal | Team |
|------|---------|
| Only manifests + ABI WASM | `MODE=lang_static bash scripts/tests/run_daily.sh` or `STATIC_ONLY=1 …/run_language_production_pack.sh` |
| Full day regression on a live node | `MODE=full` + `ADMIN_TOKEN` + `BASE`/`COORD` → `scripts/tests/run_daily.sh` |
| Locally “everything”: build, ephemeral stack, fuzz gate, then full daily | `scripts/ops/run_local_full_matrix.sh` (Phase A - `go vet`/`go test` only; task statics once in Phase C) |
| Isolated fuzz gate | `scripts/ops/fuzz_release_gate.sh` - without static manifests/WASM; in front of it if necessary `MODE=lang_static` |

## Adding/improving language for production

1. Compilation in WASM with ABI `check(i64)->i32`.
2. `go run ./tools/task_abi_check <file.wasm>`  
3. Manifest + `go run ./tools/task_manifest_lint <manifest.json>`  
4. Lines in `language_from_code_matrix.sh` / `orders_multilang_audit.sh` / compiler in [`docs/TASK_LANGUAGES.md`](TASK_LANGUAGES.md).  
5. Run **`run_language_production_pack.sh`** on a node with toolchain enabled.

## What “top companies” usually ask (without claiming certification)

These are guidelines for Due diligence, not a “we are SOC2” checklist.

| Topic | What makes sense to show |
|------|---------------------------|
| Supply Chain | `go version`, fixed dependencies (`go.sum`), where the product image is built from, who has access to the VPS. |
| Secrets | Tokens are only in the env/secret store, not in git; rotation after leaks. |
| Surface API | Rate limits, admin/P2P auth, document [`docs/SECURITY.md`](SECURITY.md). |
| Sandbox of tasks | WASM-only path, size/time/quarantine limits - see package `internal/sandbox`. |
| Availability/observability | Health/status endpoints, logs without token leakage, SQLite backups if necessary. |
| Pentest | Separate contract: scope (domains, API), prohibition of destructive without approval, report and fixes. |

Formally, **SOC2 / ISO** are processes and an auditor, not just one script; The codebase can facilitate them with transparency and tests.
