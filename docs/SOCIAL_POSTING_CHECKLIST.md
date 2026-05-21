# Чеклист публикации (Telegram · Bitcointalk · X)

## Перед постом (5 мин)

```bash
curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats | jq .
curl -fsS https://hackme.tech/api/status | jq '{version,block_height,econ_base_reward_now_hmc}'
```

- [ ] `workers` > 0, stats отвечает 200
- [ ] На https://hackme.tech/downloads.html актуальный **SHA256** установщика
- [ ] Settlement timer на VPS (если пишете про выплаты)

---

## 1. Bitcointalk (главное)

| Шаг | Действие |
|-----|----------|
| URL | https://bitcointalk.org/index.php?topic=5583373.0 |
| Тип | **Reply** в существующий ANN (не новый топик) |
| Текст | Скопировать **[BITCOINTALK_UPDATE_BBCode.txt](BITCOINTALK_UPDATE_BBCode.txt)** в режиме BBCode |
| Превью | Проверить таблицы и ссылки |
| Подпись | Опционально: Member · dev/operator |

**Не публикуйте** «гарантированный ROI» — только факты + DYOR.

---

## 2. Telegram

| Куда | Файл |
|------|------|
| Канал / группа | [TELEGRAM_POST_STRESS_POOL.md](TELEGRAM_POST_STRESS_POOL.md) блок **RU** |
| EN-чат | блок **EN** |
| Закреп | RU + ссылки на stats + downloads + BCT |

---

## 3. X (Twitter) — опционально

```
HackMe $HMC official pool — coordinator passed 10min/100-worker stress (0% hard errors).
HTTP worker (not Stratum) · https://hackme.tech
Downloads + SHA256 · Open source https://github.com/jokeez/hackme
Bitcointalk ANN: topic 5583373 · DYOR experimental RC
```

---

## 4. После публикации

- [ ] Ответить на первые вопросы в BCT в течение 24–48 ч (доверие)
- [ ] В Telegram закрепить ссылку на **BCT thread**
- [ ] При смене релиза — новый короткий пост, ссылка на `web/site/news.xml`

---

## Файлы в репозитории

| Платформа | Документ |
|-----------|----------|
| Bitcointalk BBCode | `docs/BITCOINTALK_UPDATE_BBCode.txt` |
| Bitcointalk preview | `docs/BITCOINTALK_UPDATE_STRESS_POOL.md` |
| Полный ANN (редактировать редко) | `docs/BITCOINTALK_ANN_BBCode.txt` |
| Telegram | `docs/TELEGRAM_POST_STRESS_POOL.md` |
| MPS / майнеры | `docs/MINER_WELCOME_MPS_APPROVED.md` |
