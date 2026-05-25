# Что заскринить и куда выложить (Security Note #1 + HTML reports)

## Скриншоты (3–4 штуки)

1. **HTML fuzz-отчёт (clean)** — кампания `security-note-01`  
   Fuzz → Refresh → строка **security-note-01** → **Open report**  
   На экране: заголовок *HackMe Security Report*, badge **clean**, блок Campaign + Linked order `order-security-script-push-001`.

2. **Orders — заказ завершён**  
   Вкладка Orders → `order-security-script-push-001` → status **completed** (3/3 или progress 100%).

3. **Код guard (IDE)**  
   Файл `tasks/sources/security/rust_script_push_bounds_guard.rs` — весь файл (27 строк) или нижняя половина с `check()` и комментарием про OP_PUSHDATA1 / 520.

4. **(Опционально) HTML с находками** — для контраста  
   Кампания **redteam-property-fuzz** или **redteam-oob-trap** → Open report → **warn_medium** / **fail_high** (не путать с «80 багов в Bitcoin» — см. пояснение в треде).

## Telegram (@hackme_tech)

- Текст: `docs/TELEGRAM_POST_HTML_FUZZ_REPORTS.txt` (новость про HTML)  
- Для security thread: `docs/TELEGRAM_POST_SECURITY_RESEARCH_01.txt`  
- Прикрепить скрины **1 + 2 + 3** (альбом или 2 поста: «релиз отчётов» + «security note»).

## Bitcointalk (topic 5583373)

- Текст (BBCode): `docs/BITCOINTALK_SECURITY_NOTE_01_BBCode.txt`  
- Вставить в пост блок **[code]** из того же файла (guard + reproduce).  
- Attachments: скрины 1–3; не обязательно прикреплять `.wasm` (тяжёлый) — достаточно ссылки на GitHub path.

## Код для вставки в пост (BBCode / plain)

```rust
// tasks/sources/security/rust_script_push_bounds_guard.rs (excerpt)
#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    if n <= 0 { return 0; }
    let op = (n & 0xff) as u32;
    let claimed_len = ((n >> 8) & 0xffff) as u32;
    if op == 0x4c && claimed_len > 520 { 1 } else { 0 }
}
```

Полный файл в репо: `tasks/sources/security/rust_script_push_bounds_guard.rs`

## Честная формулировка

- **Не писать:** «нашли 80 уязвимостей в Bitcoin».  
- **Писать:** useful-PoW research guard; fuzz **clean** на демо-кампании; red-team OOB — sandbox quarantine noise; HTML reports — UX для операторов и B2B.
