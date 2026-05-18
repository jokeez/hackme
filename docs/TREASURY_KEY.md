# Казначейский ключ (DevFeeAddress)

Адрес казны в консенсусе: **`HMC-719006d93916ad52`** (`internal/chain/economics.go`, поле `DevFeeAddress`). На него идёт genesis mint (50 000 HMC) и доли сетевых/платформенных комиссий по `policy_hash` в `/api/status`.

## Где лежит приватный ключ

- **Не** в git. Оператор хранит **32-байтовый Ed25519 seed** (64 hex), из которого выводится тот же `HMC-…`, что и у ноды: `sha256(pubkey_ed25519)` → первые 16 hex → префикс `HMC-`.
- Рекомендуемый локальный файл (каталог `.secrets/` уже в `.gitignore`): **`.secrets/hackme_treasury_ed25519_seed.hex`** — одна строка, только hex, права `600`.

## Смена казны перед запуском сети

1. Сгенерировать новую пару (случайный seed):  
   `go run ./tools/gen_treasury_key`  
   В выводе: `NEW_DEV_FEE_ADDRESS`, `NEW_TREASURY_SEED_HEX`, `NEW_POLICY_HASH`.
2. Подставить новый адрес в `DevFeeAddress` в `internal/chain/economics.go`.
3. Обновить ожидаемый `policy_hash` в `internal/chain/economics_test.go` (`TestLockedPolicyHash`).
4. `go test ./internal/chain ./...` и правки в README / `docs/API.md` под новый адрес.
5. **Новая цепь:** пустая `data/`, заново `POST /api/genesis`; все узлы — **один** билд и один `policy_hash`, иначе P2P отрежет пира.

## Траты с казны (биржа, ликвидность)

Подписывайте обычные `transfer_v1` с казначейского ключа (тот же формат, что у ноды: seed → pubkey → подпись). Депозит биржи — поле `to` в переводе.

## Деплой на VPS (оператор)

Я **не подключаюсь** к вашему VPS по SSH — это делаете вы. Краткий порядок:

1. **Собрать** тот же коммит, что и на всех узлах (локально уже можно взять `dist/hackme-node-linux-amd64` или пересобрать на сервере: `go build -trimpath -o /opt/hackme/hackme-node .`).
2. **Остановить** сервис, **сохранить** старый `data/`, при смене `policy_hash` — **новая** директория `data/` (или осознанный reseed).
3. **Скопировать** бинарник + `scripts/ops/systemd/hackme-node.service`, переменные из `.env.vps` (см. `scripts/ops/vps_bootstrap.sh`), **не** класть сид казны в git — только на сервер в защищённый файл (аналог `.secrets/hackme_treasury_ed25519_seed.hex`, `chmod 600`).
4. **Запустить** ноду, **`POST /api/genesis`** с admin-токеном один раз — после этого в `GET /api/status` → `economics.dev_fee_address` будет **`HMC-719006d93916ad52`**, mint 50k уйдёт на этот адрес.
5. Windows: для десктопа используйте `dist/hackme.exe` или полный zip из `scripts/release/make_release_bundle.sh`, если нужен инсталлятор.
