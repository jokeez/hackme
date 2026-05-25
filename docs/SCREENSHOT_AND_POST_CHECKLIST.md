# Публикация — всё на GitHub, BCT только ссылки

## GitHub (главная страница)

**URL после push:**  
https://github.com/jokeez/hackme/blob/main/docs/security-note-01/README.md

Там: описание, полный код guard, 2 скрина (рендерятся в Markdown).

Скрины в дереве: `docs/screenshots/security-note-01/*.png`

## Bitcointalk

1. Скопировать **`docs/BITCOINTALK_SECURITY_NOTE_01_BBCode.txt`** в Reply.
2. Картинки подтянутся с **raw.githubusercontent.com** (не нужно attach на BCT, если [img] работает).
3. Если [img] не грузится — attach 2 PNG из `docs/screenshots/security-note-01/` и оставить ссылку на GitHub.

## Telegram

Текст: **`docs/TELEGRAM_POST_HTML_FUZZ_REPORTS.txt`** (ссылка на GitHub).  
Фото: альбом 2 PNG или только ссылка в пост.

## Push

```bash
git push origin main
```

После push проверить, что картинки открываются:
- https://raw.githubusercontent.com/jokeez/hackme/main/docs/screenshots/security-note-01/01-html-report-fail_high-redteam-property-fuzz.png
