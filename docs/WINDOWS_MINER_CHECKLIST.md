# Чеклист: второй ПК (Windows) и публичный пул

Цель: **не отдельная цепь на каждом ПК**, а **участие в пуле** (VPS = канон + coordinator). Локальный «beginner solo» в сеть не подходит.

## 1. Что скачать

- Тот же **релизный канал**, что на VPS (например `release_0.1.0-rc8`), чтобы протокол и поведение совпадали.
- В папке с **`hackme.exe`** должны быть **`workerpoh.exe`** (нативный воркер для Windows; дашборд запускает его вместо bash-скрипта), **`start_hackme_dashboard.bat`**, опционально **`start_hackme_public_pool.bat`** + **`env.public_pool.example`** (шаблон `hackme.env` / `.env` с `HACKME_PUBLIC_AUTHORITY_BASE`). В репозитории скрипты: `scripts/release/windows/`.

## 2. Запуск на втором ПК

1. Распакуй zip **в одну папку** (рядом `hackme.exe` и bat).
2. Запусти **`start_hackme_dashboard.bat`** (или `start_hackme_beginner_solo.bat` — он **только** поднимает локальную ноду + браузер; «solo» в сеть не майнит, см. текст в bat).
3. В браузере `http://127.0.0.1:8080` → **шапка**: вставь **admin token** (тот же класс секрета, что принимает твой узел при `HACKME_REQUIRE_ADMIN_TOKEN`, иначе POST не пройдут).
4. **Mining** → **Start worker** (или мастер Desktop в worker-режиме): укажи **`COORD_URL`** публичного coordinator (как на VPS, часто HTTPS + путь `/pool/coordinator` — см. nginx-сниппеты в репо).
5. Нужен **coordinator token**: из переменных на VPS / то, что выдал оператор пула (`HACKME_POOL_COORDINATOR_TOKEN` или admin token, если так настроено) — иначе API вернёт `412 coordinator_token_required`.

## 3. Окружение (желательно до первого запуска)

Рядом с **`hackme.exe`** можно положить файл **`.env`** или **`hackme.env`**: при старте нода подхватит оттуда переменные, **не перезаписывая** уже заданные в системе. Удобно для `HACKME_PUBLIC_AUTHORITY_BASE` и `HACKME_POOL_COORDINATOR_TOKEN` без ручного прописывания в «переменные среды Windows».

На втором ПК в **системных переменных** или `.env` рядом с процессом (как у вас принято на VPS):

- **`HACKME_PUBLIC_AUTHORITY_BASE`** = базовый URL **command node** с VPS (как в `README.md`), чтобы кошелёк/высота в UI совпадали с сетью без P2P.
- При необходимости явно: **`HACKME_POOL_COORDINATOR_URL`**, **`HACKME_CANONICAL_CHAIN_URL`** — см. основной `README.md`, раздел Worker-mode.

## 4. Ожидания

- **Локальный `tip_height` в SQLite** на втором ПК может **отставать** от сети — это нормально без P2P; пул и «канон» в API должны быть согласованы, см. `docs/NETWORK_MODEL.md` и `scripts/ops/verify_chain_sync_snapshot.sh`.
- Баланс на втором ПК в worker-mode **не обязан** расти как при локальном PoH-блоке — начисления идут через **coordinator / settlement**; смотри подсказки на вкладке Mining / Wallet в UI.

## 5. Проверка после включения

Со второго ПК (или с VPS):

```bash
LOCAL_BASE=http://127.0.0.1:8080 bash scripts/ops/verify_chain_sync_snapshot.sh
```

(на Windows — через Git Bash / WSL, либо перенеси логику вручную: сравни `pool_target_mod` и `global work.target_mod`.)

## 6. VPS и сайт (делает оператор с доступом)

- Залить **тот же** билд/zip, что тестируешь локально; перезапустить systemd-сервисы ноды и coordinator.
- Nginx/TLS: актуальные сниппеты в репозитории (`scripts/ops/nginx/`, `tmp/hackme-site-domain.conf` и т.д.) — применить на сервере и `nginx -t && reload`.
- На странице Downloads — ссылка и **checksum** на артефакт из CI/релиза.

Итог: **да, всё может быть нормально**, если на втором ПК не ждать «отдельной цепи», а настроить **worker + токены + authority/coordinator URL** как на основном README; exe должен быть **той же версии**, что пул на VPS.

## 7. Идеальный прогон (репо → VPS → Windows)

Порядок, чтобы не гонять «устаревшие exe» и не ловить рассинхрон протокола:

1. **На машине с репозиторием (Linux/macOS/WSL):**  
   `bash scripts/ops/repo_final_selfcheck.sh`  
   Опционально глубже: `RUN_LOCAL_STACK_SMOKE=1 bash scripts/ops/repo_final_selfcheck.sh` (короткий стек coordinator+node+worker).
2. **Собрать/взять тот же релизный zip**, что пойдёт на VPS и в Downloads; зафиксировать тег/commit в заметках релиза.
3. **VPS:** выкатить артефакт, перезапустить сервисы, `nginx -t && reload`, проверить публичные GET (`/api/status`, при необходимости `GET /api/worker/settlement`).
4. **Windows (второй ПК):** распаковать **тот же** zip → `start_hackme_dashboard.bat` → admin token → **Start pool worker** с URL координатора и токеном с VPS.
5. **Сверка:** сравнить версию/билд в UI или логах с VPS; при сомнении — снова `verify_chain_sync_snapshot.sh` (см. §5).
