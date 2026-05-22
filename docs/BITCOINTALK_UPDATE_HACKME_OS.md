# Bitcointalk — HackMe OS ISO + Zero-Knowledge Start

**Thread:** https://bitcointalk.org/index.php?topic=5583373.0  
**Paste:** [BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt](BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt)  
**When:** After ISO is on https://hackme.tech/downloads.html (verify HEAD 200).

---

## Suggested reply title

`Re: [ANN] HackMe — HackMe OS live ISO · Zero-Knowledge Start · rc11g`

---

## English (plain text preview)

**Update — 22 May 2026**

We shipped a **bootable mining rig ISO** (**HackMe OS**) for amd64 live USB — HiveOS-style, but for the **HackMe HTTP coordinator** (not Stratum).

### Zero-Knowledge Start

1. Flash ISO (Etcher / `dd`) — verify **SHA256** on the downloads page.  
2. Boot → **no wallet config**.  
3. TTY1 shows a new **`HMC-…`** address + **24-word recovery phrase** (mining key, not BTC HD).  
4. Worker connects to **`https://hackme.tech/pool/coordinator`** automatically.

On-rig: `hackme-os-status` · `hackme-show-wallet` · `hackme-os-install /dev/sdX` for persistent SSD.

### Release artifacts (`0.1.0-rc11g`)

| Artifact | Notes |
|----------|--------|
| **HackMe-OS-0.1.0-rc11g-amd64.iso** | ~956 MB live image |
| **HackMe-Setup-0.1.0-rc11g.exe** | Windows + OpenCL (AMD RX 580) |
| **Linux tarball** | `hackme` + `workerpoh` + `minersign` |

**SHA256 (ISO):** `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658`  
**Downloads:** https://hackme.tech/downloads.html#hackme-os

### Pool (live now)

| | |
|--|--|
| Stats | https://hackme.tech/pool/coordinator/api/work/stats |
| Explorer | https://hackme.tech/pool/explorer |
| Hashrate | ~**2.4 GH/s** (5 active workers) |
| Difficulty (`target_mod`) | ~**4.97M** |
| Economics | Fair mode — accrual for **accepted work** (`payout_found_only=0`) |

**Hybrid signer strict:** configure signing (`minersign` / ISO auto-setup). Unsigned submits are rejected.

### QA we ran

- Full `go test ./...` — pass  
- Nightly chaos guard (5000 payouts, replay/tamper, init-worker) — pass  
- Coordinator stress (prior update): p50 **0.9 ms**, no RAM leak  
- ISO embeds production pool worker token; claim canary **HTTP 200**

**Source:** https://github.com/jokeez/hackme (Apache-2.0) · PR: ISO build pipeline fixes.

*Experimental RC — write down your recovery phrase; live USB without disk install regenerates wallet on reboot.*

---

## Русский (Telegram / internal)

См. `docs/TELEGRAM_POST_HACKME_OS.md`
