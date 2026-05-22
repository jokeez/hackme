# Bitcointalk — HackMe OS ISO + Zero-Knowledge Start

**Thread:** https://bitcointalk.org/index.php?topic=5583373.0  
**Paste:** [BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt](BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt)  
**Status:** ISO live on https://hackme.tech/downloads.html (HTTP 200) — safe to post.

---

## Reply title (copy-paste)

```text
Re: [ANN] HackMe — HackMe OS live ISO · Zero-Knowledge Start · rc11g
```

---

## Post structure (BBCode)

1. **Centered header** + TL;DR bullets  
2. **Zero-Knowledge Start** — 6-step table (download → mine)  
3. **Release table** — ISO SHA256, Windows, Linux, docs  
4. **HackMe OS vs zip** — comparison for miners coming from HiveOS mindset  
5. **Live pool snapshot** — ~2.4 GH/s, target_mod, economics, signing  
6. **QA table** — tests + ISO on CDN  
7. **Thread continuity** — stress test, OpenCL, MPS  
8. **Official links + disclaimer**

---

## English plain-text preview

**Update — May 2026 · 0.1.0-rc11g**

We shipped **HackMe OS** — a bootable amd64 live USB image for dedicated rigs. It targets the **HackMe HTTP coordinator** (not Stratum).

**Zero-Knowledge Start:** flash → boot → TTY1 shows a new **HMC-…** address and **24-word recovery phrase** → pool worker starts automatically. No manual `hackme.ini`.

| Artifact | |
|----------|--|
| HackMe-OS-0.1.0-rc11g-amd64.iso | ~956 MB |
| SHA256 | `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658` |
| Downloads | https://hackme.tech/downloads.html#hackme-os |

**Pool (live):** ~2.4 GH/s · target_mod ~4.9M · fair mode (`payout_found_only=0`) · hybrid signer strict.

**QA:** `go test ./...` pass · chaos guard pass · ISO on CDN · claim canary HTTP 200.

*Experimental RC — write down your phrase; use disk install for persistent farms.*

---

## Русский (Telegram)

См. [TELEGRAM_POST_HACKME_OS.md](TELEGRAM_POST_HACKME_OS.md)
