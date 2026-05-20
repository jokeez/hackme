# Windows: майнинг в один клик (публичный пул hackme.tech)

## Для майнера (рекомендуется — установщик)

1. Скачай **`HackMe-Setup-<версия>.exe`** с https://hackme.tech/downloads.html  
   (не папку `HackMe` из исходников GitHub).
2. Запусти установщик → **Далее** → установка в `C:\Program Files\HackMe` (по умолчанию).
3. Мастер сам:
   - скопирует `hackme.exe`, `workerpoh`, токен пула;
   - создаст `hackme.env` и ярлык **HackMe Miner** на рабочем столе;
   - запишет путь установки в реестр (`HKLM\Software\HackMe Network\HackMe`).
4. В конце отметь **Start HackMe Miner** — откроется браузер на вкладке **Mining**.
5. Держи окно майнера открытым. Воркер стартует сам через ~10 с.

**Токен пула вручную не нужен** — он в `pool.miner.token` внутри установщика.

## Альтернатива: ZIP (продвинутые)

1. Скачай **`hackme_*_windows_setup.zip`** (плоский архив).
2. Извлеки в любую папку → запусти **`setup_hackme_miner.bat`** один раз.
3. **`Start HackMe Miner.bat`** — как в установщике.

## Удаление

**Параметры Windows → Приложения → HackMe → Удалить**,  
или меню Пуск → HackMe → Uninstall.

## Если спросит admin token в дашборде

Скопируй из `hackme.env` (в папке установки) строку `HACKME_ADMIN_TOKEN=...`.

## Для оператора пула (kapa)

```bash
bash scripts/ops/rollout_coordinator_worker_token.sh   # при смене токена
VERSION=0.1.0-rc11d bash scripts/release/make_release_bundle.sh
```

На сайт выкладываются:
- **`HackMe-Setup-<ver>.exe`** — основной download для Windows;
- zip — запасной вариант.

Сборка `.exe` на Linux: Docker `amake/innosetup` (см. `scripts/release/windows/build_installer.sh`).
