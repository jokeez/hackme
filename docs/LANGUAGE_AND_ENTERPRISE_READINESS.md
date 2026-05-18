# Языки задач и «enterprise» готовность

## Что уже поддерживается (from_code / toolchain)

Один источник правды по статусу языков: **[`docs/TASK_LANGUAGES.md`](TASK_LANGUAGES.md)**  
(Rust, C/C++, Zig, TinyGo, AssemblyScript, WAT, негативные кейсы и запрет произвольных рантаймов.)

## Как прогнать существующие языки перед продом

1. **Статика без ноды** (манифесты + ABI всех `.wasm` в `tasks/`):

   ```bash
   STATIC_ONLY=1 bash scripts/tests/run_language_production_pack.sh
   ```

   Либо через общий раннер (отчёт попадает в `reports/tests/<RUN_ID>/` для `report_summary`):

   ```bash
   MODE=lang_static RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)" bash scripts/tests/run_daily.sh
   ```

   Статический языковой шаг также выполняется в начале **`MODE=full`** и **`MODE=pre_release`**.

2. **Полный языковой пакет** (нужна запущенная нода с теми же компиляторами, что на проде-toolchain VPS, плюс admin token):

   ```bash
   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 bash scripts/tests/run_language_production_pack.sh
   ```

3. **Внутри общего регресса** — уже входит в `scripts/ops/fuzz_release_gate.sh` и в `scripts/ops/run_local_full_matrix.sh` (ephemeral stack).

Порядок работ осмысленный: **сначала зелёный статический пакет → затем live-матрицы на стенде с полным PATH** → только потом добавление новых языков или ослабление gate.

## Лестница прогонов (кратко)

| Цель | Команда |
|------|---------|
| Только манифесты + ABI WASM | `MODE=lang_static bash scripts/tests/run_daily.sh` или `STATIC_ONLY=1 …/run_language_production_pack.sh` |
| Полный дневной регресс на живой ноде | `MODE=full` + `ADMIN_TOKEN` + `BASE`/`COORD` → `scripts/tests/run_daily.sh` |
| Локально «всё»: сборка, ephemeral stack, fuzz gate, затем full daily | `scripts/ops/run_local_full_matrix.sh` (Phase A — только `go vet`/`go test`; статика задач один раз в Phase C) |
| Изолированный fuzz gate | `scripts/ops/fuzz_release_gate.sh` — без статики манифестов/WASM; перед ним при необходимости `MODE=lang_static` |

## Добавление / улучшение языка для продакшена

1. Компиляция в WASM с ABI `check(i64)->i32`.  
2. `go run ./tools/task_abi_check <file.wasm>`  
3. Манифест + `go run ./tools/task_manifest_lint <manifest.json>`  
4. Строки в `language_from_code_matrix.sh` / `orders_multilang_audit.sh` / компилятор в [`docs/TASK_LANGUAGES.md`](TASK_LANGUAGES.md).  
5. Прогон **`run_language_production_pack.sh`** на узле с включённым toolchain.

## Что обычно спрашивают «топ-компании» (без претензии на сертификацию)

Это ориентиры для Due diligence, не чеклист «мы SOC2».

| Тема | Что имеет смысл показать |
|------|---------------------------|
| Цепочка поставки | `go version`, зафиксированные зависимости (`go.sum`), откуда билдится прод-образ, кто имеет доступ к VPS. |
| Секреты | Токены только в env/secret store, не в git; ротация после утечек. |
| Поверхность API | Rate limits, admin/P2P auth, документ [`docs/SECURITY.md`](SECURITY.md). |
| Sandbox задач | WASM-only путь, лимиты размера/времени/quarantine — см. пакет `internal/sandbox`. |
| Доступность / наблюдаемость | Health/status endpoints, логи без утечки токенов, бэкапы SQLite при необходимости. |
| Пентест | Отдельный контракт: scope (домены, API), запрет destructive без согласования, отчёт и фиксы. |

Формально **SOC2 / ISO** — это процессы и аудитор, не один скрипт; кодовая база может их облегчать прозрачностью и тестами.
