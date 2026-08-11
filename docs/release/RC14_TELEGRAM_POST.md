# HackMe `0.1.0-rc14` — pool fuzz customer-first (E2E proof)

## EN

**rc14 changes what the pool spends time on:** fuzzing progress for **customer orders**, not bootstrap-only starvation.

**Before → After (E2E, Aug 11):**

```text
Claim latency p50:  ~6000 ms  →  ~1–5 ms
Claim latency p95:  ~12–20 s  →  (kept low under customer-first priority)
Pool throughput:    ~4.6 done/min → ~25–32 done/min
Customer completion: ~23% share → customer-first (~4× faster than bootstrap-tier)
Customer orders:    256/256 → 256/256 (in ~26 min, ~9.8/min)
Hybrid dig:         ON (2/50/2000/10%), FINDINGs on customer campaigns
Infra packaging:    owner_ref preserved; bootstrap resync fixed
Escrow:             20/80 unchanged
```

**What to download**
- Downloads: https://hackme.tech/downloads.html
- GitHub release: https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14
- Pool: https://hackme.tech/pool/coordinator
- Fuzz guide: https://hackme.tech/fuzz-guide.html

## RU

**В `0.1.0-rc14` мы поменяли приоритет пула:** теперь время уходит на **выполнение заказов клиентов**, а не на «bootstrap-only» голодание.

**Было → Стало (E2E, 11 августа):**

```text
Claim latency p50:  ~6000 мс  →  ~1–5 мс
Claim latency p95:  ~12–20 с  →  (остается низким при customer-first приоритете)
Производительность пула: ~4.6 done/min → ~25–32 done/min
Доля customer claim:     ~23% → customer-first (~в ~4× быстрее чем bootstrap-tier)
Заказы клиентов:         256/256 → 256/256 (за ~26 мин, ~9.8/мин)
Hybrid dig:              ВКЛ (2/50/2000/10%), FINDINGs на customer campaigns
Инфра в пакете:          owner_ref сохранен; bootstrap resync исправлен
Escrow:                 20/80 без изменений
```

**Где скачать**
- Загрузки: https://hackme.tech/downloads.html
- GitHub release: https://github.com/jokeez/hackme/releases/tag/0.1.0-rc14
- Pool: https://hackme.tech/pool/coordinator
- Fuzz guide: https://hackme.tech/fuzz-guide.html

