# HackMe Private Testnet Runbook

## 1. Цель

Проверить transfer-поток (`transfer_v1`) и базовый P2P gossip в контролируемой сети из 3-10 нод до выхода в публичный интернет.

## 2. Минимальная конфигурация

- На каждой ноде:
  - `HACKME_ADMIN_TOKEN=<strong-secret>`
  - `HACKME_P2P_TOKEN=<peer-shared-secret>`
  - `HACKME_P2P_PEERS=http://ip1:8080,http://ip2:8080,...` (без себя)
  - optional discovery:
    - `HACKME_P2P_DISCOVERY=1`
    - `HACKME_P2P_ADVERTISE_URL=http://<lan-ip>:8080` (URL, который нода объявляет соседям)
    - discovery транзитивный: если сосед вернул `announce_url`, нода может добавить его как `source=discovered` (с ограничением внутреннего cap на общее число peers).
- Открыть доступ только между узлами тестнета (firewall allowlist).
- HTTPS/reverse-proxy желательно уже на этой фазе.

## 3. Порядок запуска

1. Поднять ноду A, выполнить `POST /api/genesis`.
2. Поднять ноды B/C/... с тем же `chain_id` кодовой версии.
3. Убедиться, что `GET /api/p2p/peers` не пустой минимум у части нод.
4. Отправить несколько `POST /api/tx/send` на одной ноде.
5. Проверить на соседних нодах появление tx через `GET /api/tx/pool`.
6. Запустить майнинг на одной/нескольких нодах и убедиться, что tx переходят в `included`.

## 4. Health-checks

- `GET /api/status`:
  - `tip_height` растёт,
  - `schema_version == schema_expected`.
- `GET /api/p2p/peers`:
  - есть актуальные `seen_at`,
  - нет долгих провалов связи.
- `GET /api/tx/{hash}`:
  - статусы предсказуемо переходят `pending -> included`.

### One-command preflight gate (рекомендуется перед pre_release)

```bash
ADMIN_TOKEN=... \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
scripts/ops/private_stage_gate.sh
```

Проверяет:
- schema/auth инварианты (`/api/status`);
- наличие диагностических полей sync-block (`/api/p2p/sync`);
- доступность отчёта по железу (`/api/reports/hardware?format=json`);
- health coordinator;
- опционально freeze/backup (`DO_FREEZE=1`, `DO_BACKUP=1`).

## 5. Soak-test (8-24ч)

- Нагрузка:
  - батчи transfer tx (разные отправители),
  - параллельный майнинг,
  - периодические перезапуски 1-2 нод.
- Проверки:
  - нет потери tx-history,
  - nonce не «ломается» после рестарта,
  - mempool не переполняется аномально.

## 6. Инциденты и восстановление

- Если одна нода деградировала:
  1. Остановить процесс.
  2. Снять копию `data/hackme.db`.
  3. Перезапустить с тем же env.
  4. Проверить `GET /api/status` и `GET /api/p2p/peers`.
- Если рассинхрон критичный:
  - изолировать ноду от пиров,
  - сохранить DB для анализа,
  - пересоздать из эталонного снимка/чистой БД по решению оператора.

## 7. Критерии выхода в публичный этап

- Не менее 24 часов soak без потери целостности tx-history.
- Нет необъяснимых расхождений nonce/balance между узлами тестовой группы.
- Подготовлен rollback-план и чеклист алертов.
