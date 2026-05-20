# Windows: майнинг в один клик (публичный пул hackme.tech)

## Для майнера (Ярослав и др.)

1. Скачай **`hackme_*_windows_setup.zip`** с https://hackme.tech (не старый `rc9`, не папку `HackMe` из исходников).
2. ПКМ по zip → **Извлечь всё** → открой папку.
3. Двойной клик **`setup_hackme_miner.bat`** (один раз) — установит в `C:\HackMe`, создаст `hackme.env`, ярлык на рабочем столе.
4. Двойной клик **`Start HackMe Miner.bat`** (на рабочем столе или в `C:\HackMe`).
5. Откроется браузер → вкладка **Mining**. Если спросит admin token — скопируй из `C:\HackMe\hackme.env` строку `HACKME_ADMIN_TOKEN=...`.
6. Воркер обычно стартует сам через ~10 с; иначе нажми **Start pool worker**.

**Не нужно** вручную искать «токен пула» — он уже в `pool.miner.token` внутри релиза.

## Если `Скопировано файлов: 0` или пустая папка `windows`

- Zip повреждён или скачан не до конца — скачай заново **`_windows_setup.zip`**.
- Не копируй папку `Downloads\HackMe` с исходниками Go — там нет `workerpoh.exe` и bat-файлов релиза.

## Команды cmd (не Linux)

| Linux | Windows |
|-------|---------|
| `ls` | `dir` |
| `cat file` | `type file` |

## Для оператора пула (kapa)

Перед выкладкой zip на сайт:

```bash
# один раз: worker token на hub + воркеры
bash scripts/ops/gen_coordinator_worker_token.sh   # если файла ещё нет
bash scripts/ops/rollout_coordinator_worker_token.sh

# сборка релиза (вшивает .secrets/hackme_coordinator_worker_token в pool.miner.token)
VERSION=0.1.0-rc11 bash scripts/release/make_release_bundle.sh
```

Выложить на Downloads: **`hackme_<ver>_windows_setup.zip`** (плоский архив).

Смена токена = новый worker token на VPS + пересборка zip.
