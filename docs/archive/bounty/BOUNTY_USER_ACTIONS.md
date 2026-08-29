# Что делать тебе (bounty) — 2026-06-26

Автоматика крутится в фоне (`run_bounty_resume.sh`). Этот файл — **только ручные шаги**, которые бот не закроет.

## Срочно: Tokenize.it (дедлайн **27 июня**)

1. Зарегистрируйся / войди: [dashboard.hackenproof.com](https://dashboard.hackenproof.com)
2. Вступи в программу **tokenize-it-token-sc-dualdefense-audit**
3. Подтверди scope = commit `52b0322fb566c7143d09c23b7bd30f2e092e0691`
4. **Ручной аудит** (2–4 ч) — открой в IDE:
   - `.cache/bounty-repos/tokenize-it/contracts/CoinvestedPosition.sol`
   - `contracts/Exit.sol`
   - `contracts/GlobalTokenExitRegistry.sol`
   - `contracts/Distribution.sol`
5. Ищи конкретно:
   - double-spend / over-withdraw pull credits (`leadInvestorCredit`, `coinvestorCredit`)
   - обход `lockedUntil` / recovery timer
   - неверный split `profitFraction` при exit в другой валюте
   - кто может вызвать `withdraw` / `claim` не тем получателем
   - registry: повторный exit одного токена, подмена exit price
6. Сабмит **только** если есть Foundry PoC + impact + severity по таблице программы
7. Не сабмить GSN forwarder fails / `vm.assume` noise — это уже отфильтровано в отчётах

Подробнее: [TOKENIZE_ULTRA_HUNT.md](./TOKENIZE_ULTRA_HUNT.md)

## OSS CVE (не bounty-платформы)

| ID | Статус | Твоё действие |
|----|--------|---------------|
| centijson | triage | Ждать ответ maintainer; не публиковать repro |
| libucl | hold | Отправить issue draft upstream когда будешь готов |
| cfgpack | hold | То же |

## После фонового resume

```bash
tail -f reports/bounty/CURRENT_RESUME/resume.log
cat reports/bounty/CURRENT_RESUME/rollup.json | jq .verdict
```

Если `BOUNTY_CANDIDATE` — смотри `phases/*/rollup.json` и триажь counterexample вручную.

## Не трать время

- Arcadia / 0xmarkets compile hell без fork — низкий ROI до настройки Base RPC
- Discovery repos с `compile_err` — инфра, не находки
- Повторный сабмит «fuzz failed» без PoC — отклонят
