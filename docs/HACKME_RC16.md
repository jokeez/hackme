# HackMe 0.1.0-rc16 — self-update channel + signed apt

**Status:** LIVE · [downloads](https://hackme.tech/downloads.html) · [GitHub](https://github.com/jokeez/hackme/releases/tag/0.1.0-rc16) · [latest.json](https://hackme.tech/dist/latest.json)

## Highlights

- **L1 self-update** — `latest.json` + `update_hackme_miner.sh` / `.ps1` / `.bat` (keeps `.env` / `data` / `logs`)
- **Signed apt** — `hackme-node` at `https://hackme.tech/apt` (`stable`)
- **Linux app menu** — branded **HackMe** + **HackMe Dashboard** icons
- **Dashboard** — **Updates** button → `GET /api/updates/check`; Windows shows an **update available** banner (`update_hackme_miner.bat` / Setup)
- **HackMe OS ISO** — rebuilt for rc16 (verify `SHA256SUMS-iso.txt`)

## How to upgrade

### Ubuntu / Debian (recommended)

```bash
curl -fsSL https://hackme.tech/apt/hackme-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/hackme-archive-keyring.gpg >/dev/null
echo 'deb [signed-by=/usr/share/keyrings/hackme-archive-keyring.gpg] https://hackme.tech/apt stable main' \
  | sudo tee /etc/apt/sources.list.d/hackme.list
sudo apt update && sudo apt install hackme-node
# later:
sudo apt upgrade hackme-node
```

### Linux (tar)

```bash
# from install dir, e.g. /opt/hackme or extracted linux/
bash update_hackme_miner.sh
```

### Windows

```bat
update_hackme_miner.bat
```

Prefers `HackMe-Setup-*.exe` from `latest.json`; keeps `hackme.env`.

### HackMe OS

```bash
bash update_hackme_os_binaries.sh
```

## SHA256 (core)

```
e795059cd2a5899e346d3716d4a638f4368cfb95f57b869a93f2610e359ada84  hackme_0.1.0-rc16_windows.zip
7f94002532c53aee74a7fd47b085204a4c2e9d88b091bb946a82904a598f5d91  hackme_0.1.0-rc16_windows_setup.zip
b3719ece8b9b822d45abd22a458b9f75262055838ccb74ec6be44f2844e4a357  hackme_0.1.0-rc16_linux.tar.gz
89e9424c40fcf2661f02df5cdbb0d0da53acea2afa5882ed141ac2cf95d8ff5c  HackMe-Setup-0.1.0-rc16.exe
d20c24ce36a910499fe95615a837945af881bd32e04e6596c4506a816359b0cd  hackme-node_0.1.0-rc16_amd64.deb
9e1c0850285f6fa0733aa6459fb2864dad659bf6b8e12c33d17e61f146d65f67  HackMe-OS-0.1.0-rc16-amd64.iso
```

Full lists: `SHA256SUMS.txt` · `SHA256SUMS-iso.txt`

## Links

- Downloads: https://hackme.tech/downloads.html
- Apt: https://hackme.tech/downloads.html#apt
- News: https://hackme.tech/news.html
