# Биржи: дорожная карта листинга HMC

Технический пакет для интеграции: **[EXCHANGE_LISTING_WALLET_PREP.md](EXCHANGE_LISTING_WALLET_PREP.md)** · HTTP API: **`docs/API.md`** · спека: **`spec/CHAIN_SPEC.md`**

## Этап 0 — до заявки (сейчас)

| Требование | Статус | Где проверить |
|------------|--------|----------------|
| Публичный command node + explorer | ✅ | https://hackme.tech/pool/explorer |
| `GET /api/status` (genesis, economics, policy) | ✅ | https://hackme.tech/api/status |
| Открытый GitHub | ✅ | https://github.com/jokeez/hackme |
| Bitcointalk ANN | ✅ | topic 5583373 |
| Пул в работе + stats API | ✅ | `.../pool/coordinator/api/pool/stats` |
| MiningPoolStats (модерация) | ⏳ | [MININGPOOLSTATS_LISTING.md](MININGPOOLSTATS_LISTING.md) |
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
   - Ссылка на **MiningPoolStats** (после Approved)

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

После **Approved** на miningpoolstats.app:

- В форме биржи укажите карточку пула как доказательство live network
- Следите, чтобы `api/pool/stats` не падал (робот MPS опрашивает каждые минуты)
- Шаблон для новых майнеров: **[MINER_WELCOME_MPS_APPROVED.md](MINER_WELCOME_MPS_APPROVED.md)**
