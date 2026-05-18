# Task Languages Policy (WASM ABI v1)

Этот документ фиксирует, какие языки стоит подключать для задач Orders и в каком порядке.

## Базовый принцип

Поддержка языка = компиляция в WASM с ABI:

- экспорт ровно один: `check`
- сигнатура: `check(i64) -> i32`

Если ABI не соблюдён, модуль не должен попадать в прод.

## Статус языков

| Язык | Статус | Комментарий |
|---|---|---|
| Rust | Ready now | Лучший основной язык для задач, стабильный wasm toolchain (`rustc`). |
| C | Ready now | `language: "c"`, алиас `gcc`; `clang` wasm target, как у C++ но `-x c`. |
| C++ | Ready now | `language: "cpp"`; `clang` wasm + `-Wl,--export=check`. |
| Assembly (WAT/wasm text) | Ready now (guarded) | `language: "wat"` (алиасы: `asm`, `assembly`, `wast`, `wasm-text`), `wat2wasm`. |
| Python | Later via WASM only | Нативный Python-рантайм в ноде не рекомендуется; только sandboxed wasm-путь после строгих лимитов. |
| Go (TinyGo) | Ready now (guarded) | `tinygo` (`go`/`golang`); sanitize + strict ABI gate. |
| Zig | Ready now (guarded) | `zig build-lib` → wasm32-freestanding; sanitize + gate; на узле нужен `zig` в PATH. |
| AssemblyScript | Ready now (optional toolchain) | `language: "assemblyscript"` или `as`; CLI `asc` (`npm i -g assemblyscript`); sanitize + gate. |

## Что не делать

- Не запускать произвольные `py` / `exe` / `sh` из заказов.
- Не добавлять разные ABI под разные языки.
- Не принимать артефакты без проверки ABI и hash.

## Минимальный процесс добавления нового языка

1. Компиляция в `.wasm`.
2. Проверка ABI: `go run ./tools/task_abi_check <artifact.wasm>`.
3. Подсчёт SHA-256.
4. Создание манифеста (`wasm_artifact_path` + `artifact_hash`).
5. Линт манифеста: `go run ./tools/task_manifest_lint <manifest.json>`.
6. Загрузка через `POST /api/tasks`.

## Campaign-mode (fuzz/property/symbolic)

Чтобы язык был полезен для white-hat сети, одного `POST /api/tasks` недостаточно:

- кампании создаются через `POST /api/fuzz/campaigns` (`campaign_type`: `fuzz|property|symbolic`);
- результаты/находки публикуются батчами через `POST /api/fuzz/campaigns/{id}/findings`;
- заказчику отдаётся унифицированный отчёт `GET /api/fuzz/campaigns/{id}/report` (`fuzz_report_v1`).

## Быстрые команды

```bash
bash scripts/build_task_wasm.sh
go run ./tools/task_abi_check tasks/artifacts/rust_check.wasm tasks/artifacts/cpp_check.wasm
go run ./tools/task_manifest_lint tasks/manifests/order-rust-001.json tasks/manifests/order-cpp-001.json
```

Полный регресс по **всем** манифестам/wasm и live-матрицам языков (при поднятой ноде): см. **`scripts/tests/run_language_production_pack.sh`** и **[`docs/LANGUAGE_AND_ENTERPRISE_READINESS.md`](LANGUAGE_AND_ENTERPRISE_READINESS.md)**.
