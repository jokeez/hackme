# HackMe — модель безопасности (MVP / локальный узел)

Документ задаёт **честные ожидания**: что защищено сейчас, что сознательно не делается до выхода в сеть, и **чеклист** на момент, когда узел станет доступен не только с localhost.

Это не аудит «как у банка» и не юридическая гарантия.

---

## 1. Текущая модель доверия

| Аспект | Поведение |
|--------|-----------|
| Сеть | HTTP-сервер слушает **`127.0.0.1`** — удалённый интернет **не** достучится до API, пока вы сами не пробросите порт / reverse-proxy. |
| Пользователь ОС | Любой процесс **от вашего имени** на той же машине может вызывать `http://127.0.0.1:8080/...` так же, как браузер. |
| «Кошелёк» | Одна строка **`wallet`** в SQLite (`balance_hmc`) — **учёт внутри ноды**, не отдельный аппаратный/HSM-кошелёк. |
| Ключ **Ed25519** (`data/node_ed25519.seed`) | Подпись ответов API (например заказов), права доступа к файлу — у пользователя ОС. |
| Дашборд | Статическая страница с `localhost`; секрет не встраивается в HTML. Для админ-действий токен задаётся локально на клиенте (`localStorage.hackme_admin_token`). |

---

## 2. Что уже есть (мигация угроз)

- **Admin token policy:** по умолчанию запуск требует **`HACKME_ADMIN_TOKEN`** (`HACKME_REQUIRE_ADMIN_TOKEN=1`). Явное ослабление только для локальной отладки: `HACKME_REQUIRE_ADMIN_TOKEN=0`.
- Мутирующие **POST** требуют заголовок **`X-Hackme-Admin-Token: <token>`** или **`Authorization: Bearer <token>`**. При ошибке — **401 Unauthorized** с `WWW-Authenticate`.
- Защищаются: **`POST /api/genesis`**, **`POST /api/mining/start`**, **`POST /api/mining/stop`**, **`POST /api/worker/start`**, **`POST /api/worker/stop`**, **`POST /api/tasks`**, **`POST /api/tasks/from_code`**, **`POST /api/push_work`** (тело до **1 MiB**, как и для прочих крупных JSON), **`POST /api/hardware/tune`**, а также админ-ветки **fuzz** (см. `fuzz_campaigns.go`). Чтение (GET метрики, цепь, логи, SSE логов, **`GET /api/hardware/tune`**, **`GET /api/worker/status`**) — без токена.
- Для private testnet P2P: при заданном **`HACKME_P2P_TOKEN`** эндпоинты **`/api/p2p/*`** требуют `X-Hackme-P2P-Token`.
- Для anti-spam введён базовый rate-limit: `POST /api/tx/send`, `POST /api/tasks`, `POST /api/p2p/tx`, `POST /api/push_work` получают **429** при всплеске.
- Добавлены anti-drain лимиты по эскроу заказов: `HACKME_MAX_ORDER_ESCROW_PER_HOUR_HMC` (по умолчанию ограничение в час).
- **SQLite `PRAGMA user_version`** — версия схемы после миграций; в **`GET /api/status`**: `schema_version`, `schema_expected`.
- **WASM:** таймаут на вызовы, лимит размера check-модуля, лимит памяти рантайма wazero (см. `internal/sandbox`). Файлы заказов: только под **`tasks/artifacts/`** (или **`HACKME_TASK_ARTIFACT_DIR`**), относительный путь без `..`.
- **WASM hardening:** строгая проверка заголовка/версии модуля, только экспорт `check(i64)->i32`, пробный вызов при валидации, quarantine невалидных модулей по hash (по умолчанию блокируется повторная загрузка quarantined хеша). Настройки: `HACKME_SANDBOX_MAX_CHECK_WASM_BYTES`, `HACKME_SANDBOX_CHECK_TIMEOUT_MS`, `HACKME_SANDBOX_BLOCK_QUARANTINED`.
- **`.gitignore`:** `data/node_ed25519.seed` — не коммитить ключ.
- **Блоки PoH / синхронизация:** при записи блока проверяется согласованность поля `hash` с заголовком (`verifyBlockIntegrityAndSignature` в `internal/chain/service.go`). Подпись майнера **Ed25519** проверяется по сообщению **`hash` блока**, если поля подписи заданы; **полностью пустые** поля подписи допускаются для совместимости с историческими цепочками без подписи. На пути P2P staging/apply при наличии подписи действует та же логика (`verifySyncBlockSignature` в `main.go`); подделка `hash` или подписи под другой ключ отсекается. Направление ужесточения: опциональный режим «все новые блоки только signed» (отдельная задача/флаг).
- **Локальный WASM PoH по HTTP:** только если процесс запущен с **`HACKME_CHAIN_LEADER_LOCAL_POH=1`** (command-node). Обычные узлы / участники пула майнят через **`POST /api/worker/start`** и **`HACKME_POOL_COORDINATOR_URL`**. **`HACKME_BEGINNER_SOLO`** удалён (см. `docs/BEGINNER_SOLO.md`).

---

## 3. Сознательно не в MVP (до сети не обещаем)

- P2P-аутентификация, защита от replay между нодами, консенсус по «чужим» блокам.
- TLS на HTTP (для чистого localhost часто избыточен; при прокси — TLS на прокси).
- Rate limiting / WAF на API.
- Полноценная p2p аутентификация и репутация пиров (сейчас baseline handshake + token).
- Шифрование SQLite «на диске» без пароля пользователя даёт мало против того же пользователя ОС.
- Отмена заказов и возврат эскроу — отдельная экономическая и протокольная модель (см. `README_ROADMAP.md`).

Demo / current emission policy:

- Genesis mints **50 000 HMC** once to the consensus treasury address `DevFeeAddress` (`internal/chain/economics.go`).
- Further emission is allowed via validated PoH block rewards and order flows under the supply cap.

---

## 4. Когда вынесете узел в LAN / интернет — чеклист по факту

1. **Поверхность:** кто может достучаться до TCP (только VPN? только LAN? публичный IP?).
2. **Транспорт:** TLS (или mTLS), доверенный reverse-proxy, отключение слабых шифров. Опционально **`HACKME_HTTP_CORS_ALLOW_ORIGIN`** только если осознанно нужен cross-origin доступ к `/api/*` из браузера; иначе не задавать.
3. **Аутентификация:** не полагаться только на `HACKME_ADMIN_TOKEN` в HTML — для продакшена отдельные роли, короткоживущие токены, отсутствие секрета в разметке страницы.
4. **Секреты:** отдельный пользователь ОС для процесса, ограничение прав на `data/*.db` и `*.seed`.
5. **Лимиты:** размер тела POST, частота запросов, таймауты WASM, запрет опасных импортов в пользовательском WASM.
6. **Наблюдаемость:** логи без утечки токенов; алерты на аномальный расход эскроу.
7. **Угрозы сети:** при появлении P2P — подпись блоков, идентичность пиров, анти-replay, обновления безопасности зависимостей.
8. **Transfer-защита:** проверка подписи, nonce anti-replay, баланс+fee и мониторинг аномалий `429/invalid_signature/invalid_nonce`.

Практический предзапускной gate перед интернет-экспозицией:  
`scripts/ops/internet_preflight.sh` (проверяет sandbox/economics/status, security headers, difficulty health, p2p/sync/coordinator readiness и сводит PASS/FAIL в `reports/gates/<run_id>`).

---

## 5. Связанные файлы

| Файл | Смысл |
|------|--------|
| `admin_auth.go` | Проверка `HACKME_ADMIN_TOKEN` |
| `main.go`, `pool.go` | Маршруты, вызов `requireAdminAuth` |
| `internal/store/sqlite.go` | Версия схемы |
| `internal/nodecrypto/` | Ключ подписи API |
| `spec/CHAIN_SPEC.md`, `docs/API.md` | Протокол и HTTP |

**Отдельный процесс `cmd/coordinator`:** по умолчанию слушает **127.0.0.1:8081**. Если задан **`HACKME_COORDINATOR_ADMIN_TOKEN`**, мутирующие **`POST /api/push_work`**, **`POST /api/work/claim`** и **`POST /api/work/submit`** требуют **`X-Hackme-Admin-Token`** или **`Authorization: Bearer ...`** (тот же стиль, что и у command node). Без токена эти POST принимаются от любого клиента, достигшего bind-адреса — для продакшена задайте токен, держите bind на loopback/VPN или включите **`HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=1`** (тогда процесс не стартует, пока пустой `HACKME_COORDINATOR_ADMIN_TOKEN`).

При смене политики безопасности обновляйте этот файл и **`docs/API.md`**.
