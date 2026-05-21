# Telegram — пост обновление (закреп / канал)

Перед публикацией подставьте **актуальные** цифры:
`curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats | jq '{hashrate,workers}'`

---

## RU (основной)

```
🟢 HackMe Pool — стресс-тест координатора пройден

Публичный пул: https://hackme.tech
Статистика: https://hackme.tech/pool/coordinator/api/pool/stats
Эксплорер: https://hackme.tech/pool/explorer

Что проверили (10 мин, 100 виртуальных воркеров):
• 0% критических ошибок, координатор не падал
• Память ~12→20 MB, утечки нет
• Халвинг блок 2 102 401: награда 0.01 → 0.005 HMC ✓
• 1000 битых запросов — мгновенный отказ, без зависаний

⚠️ Это не Stratum — майним через hackme-node + workerpoh (HTTP coordinator).

Скачать (SHA256 на сайте):
https://hackme.tech/downloads.html
• Windows: HackMe-Setup (OpenCL для AMD RX 580)
• Linux: CUDA / OpenCL / CPU

Код: https://github.com/jokeez/hackme
ANN Bitcointalk: https://bitcointalk.org/index.php?topic=5583373.0

Вопросы: support@hackme.tech
DYOR — RC-софт, проверяйте выплаты в эксплорере.
```

---

## EN

```
🟢 HackMe Pool — coordinator stress test passed

Public pool: https://hackme.tech
Stats: https://hackme.tech/pool/coordinator/api/pool/stats
Explorer: https://hackme.tech/pool/explorer

Stress run (10 min, 100 virtual workers):
• 0% hard errors, no coordinator crash
• RAM ~12→20 MB, no leak
• Halving at block 2,102,401: 0.01 → 0.005 HMC ✓
• 1000 malformed requests — instant reject

⚠️ Not Stratum — mine with hackme-node + workerpoh (HTTP coordinator).

Downloads (verify SHA256 on site):
https://hackme.tech/downloads.html
• Windows: HackMe-Setup (OpenCL for AMD RX 580)
• Linux: CUDA / OpenCL / CPU

Source: https://github.com/jokeez/hackme
Bitcointalk: https://bitcointalk.org/index.php?topic=5583373.0

support@hackme.tech · experimental RC — DYOR
```

---

## Короткий (Stories / repost)

```
HackMe $HMC — пул на hackme.tech
Стресс координатора: 100 воркеров / 10 мин — стабильно
Майнинг: не Stratum, workerpoh + node
📥 downloads.html · 📊 pool stats API · 💬 BCT topic 5583373
```
