# HackMe — мастер-план развития

Документ объединяет исходный план («скелет блокчейна → леджер → WASM»), текущее состояние кода и **пробелы**, которые нужно закрыть для зрелой системы. Файл плана в Cursor не заменяет: это живая дорожная карта **в репозитории**.

---

## 1. Видение и границы MVP

**Цель продукта:** локальный «узел командования» с реальной цепочкой блоков на диске, наградой за генезис и демонстрацией **Proof-of-Hack** как поиска решения в **WASM**-замке (не ломать чужие системы — только синтетические задачи в песочнице).

**Вне scope MVP:** mainnet, P2P-синхронизация, ZK-доказательства, листинги бирж, B2B-заказы аудита.

---

## 2. Карта каталогов (укомплектование)

```
HackMe/
├── README.md                 # входная точка
├── go.mod / go.sum
├── main.go                   # HTTP, wiring, маршруты
├── dashboard.html            # UI (embed)
├── metrics.go                # /api/metrics (gopsutil + nvidia-smi)
│
├── docs/                     # человеческая документация
│   ├── MASTER_PLAN.md        # этот файл
│   ├── ARCHITECTURE.md       # архитектура и потоки
│   ├── API.md                # контракт HTTP
│   └── SECURITY.md           # модель угроз MVP, токен админа, чеклист на сеть
│
├── spec/                     # нормативные спецификации (форматы, правила)
│   └── CHAIN_SPEC.md
│
├── internal/                 # код приложения (не импортируется извне)
│   ├── block/                # структура блока, SHA-256, генезис
│   ├── chain/                # сервис цепи, кошелёк, майнер
│   ├── store/                # SQLite, миграции
│   └── sandbox/              # WASM eval (wazero)
│
├── data/                     # runtime: hackme.db (не коммитить при желании)
├── tools/                    # заготовка: скрипты сборки, codegen WASM
├── scripts/                  # заготовка: деплой, бэкап БД
└── testdata/                 # заготовка: бинарные .wasm для будущих тестов
```

**Правило:** всё новое по домену класть в `internal/<домен>`; «что и зачем» — в `docs/` и `spec/`.

---

## 3. Исходный план — статус выполнения

### Фаза 1 — Скелет блокчейна (данные + генезис + кнопка)

| Требование | Статус | Где в коде |
|--------------|--------|------------|
| `Task`, `Block`, `Index`, `Timestamp`, `Hash`, `PrevHash`, `Nonce`, `MinerAddress` | **Сделано** | `internal/block/types.go` |
| Каноническая сериализация + SHA-256 | **Сделано** | `internal/block/hash.go` |
| Генезис, `PrevHash` = 64 нуля, награда 0 HMC (production policy) | **Сделано** | `internal/block/genesis.go` |
| `POST /api/genesis`, повтор → 409 | **Сделано** | `main.go`, `internal/chain/service.go` |
| Лог сервера с хэшем + UI | **Сделано** | `main.go`, `dashboard.html` |

**Дополнение к плану (рекомендуется дальше):** версия схемы блока (`schema_version`), явное поле `reward` в блоке для аудита эмиссии.

### Фаза 2 — Локальное хранилище

| Требование | Статус | Где в коде |
|--------------|--------|------------|
| SQLite без CGO | **Сделано** | `modernc.org/sqlite`, `internal/store/sqlite.go` |
| Таблицы blocks / meta / wallet | **Сделано** | миграции в `store.Open` |
| `GET /api/wallet`, `GET /api/chain` | **Сделано** | `main.go` |
| Загрузка состояния после рестарта | **Сделано** | UI: `refreshWallet` / `refreshStatus` |

**Пробелы:** бэкап БД, экспорт цепи в файл. ~~`PRAGMA user_version`~~ — `internal/store.CurrentSchemaVersion` + bump в `migrate()`; видно в `GET /api/status` как `schema_version` / `schema_expected`.

### Фаза 3 — WASM-песочница и майнинг

| Требование | Статус | Где в коде |
|--------------|--------|------------|
| wazero, минимальный модуль | **Сделано** | `internal/sandbox/eval.go` (hex-модуль `eval`) |
| Воркер перебора, лог попыток | **Сделано** | `internal/chain/miner.go` |
| UI Mining + логи (SSE + откат на polling) | **Сделано** | `dashboard.html`, `GET /api/mining/logs/stream`, `GET /api/mining/logs` |

**Пробелы относительно «идеала» плана:**

- **SSE** только для **логов майнинга**; телеметрия и графики — по-прежнему **polling** `GET /api/metrics`.
- Нет **`RunLock(input []byte)`** с таймаутом и лимитом fuel — сейчас только `Eval(nonce uint64)`; следующий шаг: обёртка `context.WithTimeout` + счётчик шагов/ fuel API wazero.
- WASM зашит **hex** в коде, не `testdata/*.wasm` — для команды удобнее положить артефакт в `testdata/` и `//go:embed`.

---

## 4. Chain ID и именование сети

Константа: **`hackme-dev-mainnet`** (`internal/block/genesis.go`). Отображается в шапке дашборда.

**Рекомендация:** для публичного тестнета позже завести `hackme-testnet-1` и вынести в конфиг (`HACKME_CHAIN_ID`).

---

## 5. Расширенный бэклог (чего не хватало в коротком плане)

Приоритет сверху вниз — настраиваемый, но логичный порядок.

### A. Целостность и безопасность узла

- Подпись блоков / транзакций (Ed25519 или ECDSA), отдельный `internal/wallet`.
- Проверка цепи при старте (rehash от генезиса до tip).
- Лимиты на размер JSON блока, rate limit на API.

### B. Консенсус и сеть (после стабилизации одного узла)

- P2P gossip (libp2p или самописный UDP/TCP), общий `internal/net`.
- Синхронизация: запрос блоков по высоте / по хэшу.

### C. Proof-of-Hack «по-взрослому»

- Задачи как **WASM + manifest** (лимиты памяти, таймаут, версия ABI).
- Верификация решения всеми нодами одинаково (детерминизм).
- Опционально: **ZK** только после фиксации языка утверждений (что именно доказываем).

### D. Продукт и операции

- Конфиг YAML/ENV (`internal/config`).
- Логи структурированные (slog), уровни.
- Метрики Prometheus с `/metrics`.

### E. Юридика и этика (для реальных заказчиков)

- Только код с **согласием** владельца; техническая модель угроз — `docs/SECURITY.md`; отдельная юридическая политика — при необходимости вне репо.
- Не позиционировать сеть как инструмент взлома третьих лиц.

### F. Качество

- `go test ./...` в CI, `golangci-lint`.
- E2E: поднять сервер, `POST /api/genesis`, проверить БД.

---

## 6. Следующие конкретные шаги (итерация 2–3)

1. Вынести **таймаут WASM** и **лимит** в `internal/sandbox` + тест на зависание.
2. Добавить **`testdata/lock.wasm`** + `go:embed`, убрать дублирование hex (или codegen в `tools/`).
3. ~~**Блок #1+ при успешном PoH**~~ — `chain.Service.AppendPoHBlock`. ~~**Динамический `poh_target_mod`**~~ — `internal/chain/retarget.go`. ~~**Манифесты `./tasks` + `TaskProvider`**~~ — `internal/chain/taskprovider.go`. ~~**Заготовка пула (mock + `push_work` + UI Hive)**~~ — `pool.go`, `dashboard.html`, `/api/network/stats`.
4. **LAN coordinator** (или первый P2P gossip): подмена mock в `/api/network/stats`, выдача work воркерам.
5. ~~**SSE** для логов майнинга~~ — `GET /api/mining/logs/stream`. Дальше: SSE/WebSocket для метрик или оставить polling.
6. ~~**`PRAGMA user_version`**~~ — см. `internal/store/sqlite.go` (`CurrentSchemaVersion`); при смене схемы — новый шаг в `migrate()` и bump константы.

---

## 7. Риски (кратко)

| Риск | Митигация |
|------|-----------|
| Двойной генезис | UNIQUE `block_index`, 409 API |
| WASM DoS | таймаут `context`, `RuntimeConfig.WithMemoryLimitPages` на рантаймах песочницы (см. `internal/sandbox`) |
| Потеря `data/hackme.db` | бэкап в `scripts/`, документировать |
| Регуляторные риски токена | не ICO, прозрачный код, utility-фокус в доках |

---

## 8. Связь с артефактами

- Детали HTTP → [API.md](API.md)
- Модули и диаграммы → [ARCHITECTURE.md](ARCHITECTURE.md)
- Байт-уровень блока и хэш → [../spec/CHAIN_SPEC.md](../spec/CHAIN_SPEC.md)

*Обновляйте этот файл при смене фаз или появлении новых модулей.*
