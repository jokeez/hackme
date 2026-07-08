# Биржи: дорожная карта листинга HMC

Технический пакет для интеграции: **[EXCHANGE_LISTING_WALLET_PREP.md](EXCHANGE_LISTING_WALLET_PREP.md)** · HTTP API: **`docs/API.md`** · спека: **`spec/CHAIN_SPEC.md`**

**Публичные материалы (экосистема):** [listing.html](https://hackme.tech/listing.html) · [token-transparency.html](https://hackme.tech/token-transparency.html) · [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) · [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) · PDF: [DOCUMENTATION_EXPORT.md](DOCUMENTATION_EXPORT.md)

## Этап 0 — до заявки (сейчас)

| Требование | Статус | Где проверить |
|------------|--------|----------------|
| Публичный command node + explorer | ✅ | https://hackme.tech/pool/explorer |
| `GET /api/status` (genesis, economics, policy) | ✅ | https://hackme.tech/api/status |
| Открытый GitHub | ✅ | https://github.com/jokeez/hackme |
| Bitcointalk ANN | ✅ | topic 5583373 |
| Пул в работе + stats API | ✅ | `.../pool/coordinator/api/pool/stats` |
| MiningPoolStats (HMC coin page) | ⚠️ Closed Jul 2026 | Hosted dashboard shut down — use pool stats API instead |
| Стабильный settlement on-chain | ✅ | `hackme-worker-settlement.timer` (30s) на hub VPS |

Проверка одной командой:

```bash
PUBLIC_BASE=https://hackme.tech NODE_SSH=hackme-vps bash scripts/ops/mps_listing_readiness.sh --vps
```

## Этап 1 — стартовые PoW-биржи (порядок)

| Биржа | Зачем | Ориентир срока | Listing fee (ориентир) |
|-------|--------|----------------|-------------------------|
| **[Xeggex](https://xeggex.com)** | Основная аудитория GPU/PoW майнеров | 3–14 дней (платный) / недели (голосование) | ~$500–2000 USDT (уточнять на сайте) |
| **[NonKYC.io](https://nonkyc.io)** | Кастомные сети, быстрая техподдержка | 1–2 недели | по запросу |
| **[TradeOgre](https://tradeogre.com)** | Классика low-cap PoW | очередь / платный | умеренный |
| **CoinEx** | Этап 2 — когда есть объём с первых трёх | 1–2 месяца после объёма | выше |

Не целиться в Binance/Bybit на старте — нужны объём, аудит, юрлицо.

## Этап 2 — что отправить в форму листинга

1. **Integration pack (ZIP или ссылки)**
   - `spec/CHAIN_SPEC.md`
   - `docs/API.md` (раздел transfers)
   - `docs/EXCHANGE_LISTING_WALLET_PREP.md`
   - Ссылка на explorer: https://hackme.tech/pool/explorer
   - Ссылка на **MiningPoolStats** — https://miningpoolstats.app/coins/HMC (live)

2. **Описание монеты**
   - Useful PoW / PoH, WASM tasks, coordinator pool (не Stratum)
   - Max supply / halving — из `GET /api/status` → `economics`

3. **Соцсети**
   - GitHub, Bitcointalk ANN, Telegram, Discord — https://hackme.tech/contacts.html

4. **Депозитный тест**
   - Отдельный кошелёк биржи (`minersign -gen-seed`)
   - Тестовый `transfer_v1` + подтверждение в explorer

## Этап 3 — после первой биржи

- Указать рыночную цену HMC в калькуляторе дашборда (поле HMC $)
- CoinGecko / CoinMarketCap — отдельная заявка (нужны торги + независимые ноды)
- Не обещать ROI в рекламе — только live stats и калькулятор

## Чего нет «из коробки» (не обещать бирже без разработки)

- Нативный **Stratum** TCP (у нас HTTP coordinator + workerpoh)
- BEP-20 / ERC-20 обёртка (только нативная цепь HMC)
- Light wallet в App Store (есть desktop + worker binaries)

## Связь с MiningPoolStats

**miningpoolstats.app closed (Jul 2026)** — HMC page no longer live. Do not cite MPS in exchange forms until a replacement aggregator lists HMC.

- Use **pool stats API** as proof of live network: `https://hackme.tech/pool/coordinator/api/pool/stats`
- Optional: resubmit to [miningpoolstats.stream](https://miningpoolstats.stream) if moderators accept HTTP coordinator pools
- CEX outreach is **post-summer 2026** priority — after OSS CVE Watch + miner traction (see [roadmap.html](https://hackme.tech/roadmap.html))
