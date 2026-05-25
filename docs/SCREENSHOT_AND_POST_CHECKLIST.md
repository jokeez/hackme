# Публикация Security Note #1 + HTML reports (готово к посту)

Скрины сохранены в репо: `docs/screenshots/security-note-01/`

| Файл | Что на нём |
|------|------------|
| `01-html-report-fail_high-redteam-property-fuzz.png` | HTML-отчёт **FAIL_HIGH**, кампания `redteam-property-fuzz`, top issues |
| `02-fuzz-campaigns-dashboard.png` | Таблица Fuzz campaigns (96/96 completed, operator UI) |

## Telegram (@hackme_tech)

**Текст:** `docs/TELEGRAM_POST_HTML_FUZZ_REPORTS.txt`  
**Картинки:** оба PNG из `docs/screenshots/security-note-01/` (альбом: сначала отчёт, потом дашборд).

Опционально второй пост только про Security Note #1: `docs/TELEGRAM_POST_SECURITY_RESEARCH_01.txt` + скрин кода `rust_script_push_bounds_guard.rs`.

## Bitcointalk (topic 5583373)

**BBCode:** `docs/BITCOINTALK_SECURITY_NOTE_01_BBCode.txt` — вставить целиком.  
**Вложения:** загрузить те же 2 PNG (отчёт + Fuzz tab).  
**Код:** уже внутри BBCode в блоке `[code]`; файл `tasks/sources/security/rust_script_push_bounds_guard.rs`.

## Честные формулировки

- Скрин **FAIL_HIGH** — демо property-fuzz на `rust_bounds_guard` (много `check returned 0`, один synthetic div0-class hit). Это **не** «80 багов в Bitcoin Core».
- Security Note #1 (push bounds) — отдельный useful-PoW order; fuzz-кампания `security-note-01` была **clean** (guard ок).
- HTML-отчёты — продуктовая фича для операторов и B2B.

## Автопост новости на канал (VPS)

```bash
NODE_SSH=hackme-vps bash scripts/ops/publish_news_to_telegram.sh
```

(Новость уже в `web/site/assets/news.json` на hackme.tech.)
