# Угрозы пулу: вердикт по коду (честно)

Сопоставление «как бывает в классических статьях» с **тем, что реально делает HackMe** сегодня. Координатор: `cmd/coordinator/work.go`. Settlement: `scripts/ops/settle_worker_payouts.sh`. Retarget цепи: `internal/chain/retarget.go`. Hybrid signer: env координатора (`HACKME_POOL_HYBRID_*`).

---

## 1. «Пустые шары» / fake attempts (воркер врёт про attempts)

**Идея атаки:** взять lease, не считать, прислать submit с `found=false` и завышенными `attempts`, получить `rewardPerM * attempts / 1e6`.

**Что делает координатор сейчас**

- Платёж за попытки при `found=false`: `paidAttempts` берётся из запроса, **но ограничивается** `batch_size` lease (`attempts > req.BatchSize` обрезается) — см. `submit()` в `work.go` (~744–750).
- **Криптографической перепроверки всего диапазона** (пересчёт PoH по каждому nonce в батче) **нет**. Пересчитывается и жёстко проверяется **только** ветка **`found=true`**: диапазон `found_nonce`, `validFoundNonceV1` (eval % `target_mod`), дедуп `found_nonce` / `result_hash`.
- Защита от спама: **rate limit** claim/submit per worker и per IP, **bad strikes → временный бан** на явно плохих причинах submit (`markSubmitOutcome`: неверный work_id, подпись, replay, неверный found_nonce и т.д.) — не на «медленный honest».
- Режим **`HACKME_COORDINATOR_PAYOUT_FOUND_ONLY`**: при `found=false` **`paidAttempts=0`** — тогда оплата только за реально доказанный hit + bonus. Это **сильнейший** ответ на fake attempts **без** полной верификации каждой попытки (дорого).

**Вердикт**

| Конфигурация | Fake attempts на оплату «просто за attempts» |
|--------------|-----------------------------------------------|
| Публичный пул с **hybrid strict** + подписанные payload | Сложнее подделать **payload** без ключа; но **сама работа** по-прежнему не пересчитывается nonce-за-nonce. |
| **`payout_found_only=1`** | **Практически закрывает** сценарий «платят за воздух без hit». |
| По умолчанию (`found` может быть false, оплата за attempts) | **Доверие к числу attempts** в пределах lease; экономический риск — **операторский** (лимиты, found-only, мониторинг `total_payout_hmc` vs канон). |
| «Случайная 1% перепроверка батча» в коде | **Нет** — не реализовано. |

---

## 2. Settlement: double spend / гонки параллельных скриптов

**Идея атаки:** два процесса settlement, гонка nonce, двойная выплата до обновления state.

**Что делает `settle_worker_payouts.sh`**

- State: **JSON-файл** (`STATE_FILE`), не SQLite с `SERIALIZABLE`.
- Логика: прочитал coordinator → для воркера посчитал delta → `POST /api/tx/send` с `next_nonce` с ноды → **после успеха** обновил JSON через `jq` + `mv`.
- **Цепь:** повторная отправка с тем же nonce должна отвергаться логикой ноды (если первая транзакция уже в пуле/цепи) — это **второй рубеж**, но не замена атомарности.

**Улучшение в репозитории**

- Скрипт **`scripts/ops/settle_worker_payouts.sh`** в начале берёт **`flock`** на файл **`${STATE_FILE}.flock`**: второй параллельный процесс на том же хосте **сразу выходит** (типичный overlap cron), не конкурируя за nonce и `jq`+`mv` state.

**Вердикт**

| Механизм | Есть? |
|----------|--------|
| Serializable TX в БД settlement | **Нет** (файл JSON + curl) |
| Блокировка «pending» на воркера в state до конца tx | **Нет** (обновление после успеха) |
| Защита от двух процессов **на одном хосте** с одним `STATE_FILE` | **`flock`** — **да** (`settle_worker_payouts.sh`) |
| Защита от двух **разных** хостов с одним payer | **Нет** — не запускайте два settlement с одним кошельком без внешней координации |

---

## 3. Манипуляция сложностью / `rewardAuto`

**Идея:** скачок метрик command-node → координатор пересчитал `rewardPerM` невыгодно оператору.

**Что есть**

- Координатор периодически тянет `/api/metrics` с `HACKME_COORDINATOR_TARGET_SOURCE_URL`, обновляет `target_mod`, `base_reward_hmc`, и при **`reward_auto`** пересчитывает `rewardPerM = base_reward * 1e6 / target_mod` — см. `refreshTargetMod` в `work.go` (~313–376).
- **На цепи** retarget PoH использует **окна и лимиты шага** (`poHRetargetMaxStepUp/Down`, micro-step) — `internal/chain/retarget.go` — это **сглаживание сложности блоков**, не то же самое, что сглаживание **тарифа воркеров** в координаторе.

**Вердикт**

- Отдельного «скользящего среднего rewardPerM на 100 блоков» в координаторе **нет**; смягчение — **частота опроса**, **лимиты retarget на цепи**, ручной **`HACKME_COORDINATOR_REWARD_PER_M_ATTEMPTS`** если auto отключить.
- Риск «пул платит больше канона» — **операторский**: сравнивать `total_payout_hmc`, накопление, политику `payout_found_only`, лимиты.

---

## 4. Кража / подмена state settlement

**Идея:** доступ к диску → подмена `worker_settlement_state.json` → повторные или чужие выплаты.

**Что есть**

- Файл **не шифруется**; безопасность = **права ОС**, отдельный пользователь systemd, **бэкапы**, не хранить state на общем NFS без контроля.
- **Независимый append-only аудит** внутри скрипта **не ведётся** (есть только stdout/journal при логировании cron).

**Вердикт**

- Требования «шифрование state + независимый журнал транзакций» — **частично на стороне эксплуатации** (журнал `journalctl`, внешний SIEM, immutable backup). В коде settlement — **минимальный** след (hash tx в state после успеха).

---

## 5. Куда смотреть оператору

1. Публичный пул: рассмотреть **`HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1`** если приемлема оплата в основном за hits.  
2. Hybrid: `docs` по hybrid signer + smoke `scripts/ops` (см. `OPERATOR_FINAL_CHECKLIST.md`).  
3. Settlement: **один** cron, **flock**; мониторинг `settlement_healthcheck.sh`; бэкап state.  
4. Сверка: накопленные выплаты координатора vs баланс/эмиссия канона — вручную или скриптами мониторинга.

Документ можно использовать как ответ на вопрос «мы защищены как в статье X?» — **только с таблицами выше**, без обещаний отсутствующих механизмов.
