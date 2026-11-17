# HackMe 0.1.0-rc20

**Channel:** `0.1.0-rc20` · **Date:** 2026-11-17 · **GitHub:** https://github.com/jokeez/hackme/releases/tag/0.1.0-rc20

## Highlights

- **Paid pool Dig/Hunt** — customer Fund → hub prepaid escrow → Dig/Hunt on the public pool (hub escrow). Cancel/finalize refunds unused escrow to prepaid.
- **Authoritative reports** — Orders mirrors proxy hub `report` / `gate` / `pulse` / `csv` / `proof-bundle` (no empty local mirror).
- **Honest prepaid UI** — when `HACKME_ORDERS_HUB_URL` is set and the hub is unreachable, spendable shows `0` (`hub_prepaid_unavailable`); no local-mirror fake budget.
- **E2E gates** — customer pool / Hunt / demo scripts fail closed without miner pickup (override only with `ALLOW_SOFT_PICKUP=1`); fund waits for on-chain inclusion.
- **Full installer set** — Windows Setup + ZIP, Linux `.tar.gz` / `.deb` / `.rpm` / Arch `.pkg.tar.zst`, HackMe OS ISO, fuzzing CLIs (`hackme-fuzzing` + `hackme-fuzzing-build`).

## Artifacts

| Platform | Artifact |
|---|---|
| Windows | `HackMe-Setup-0.1.0-rc20.exe` (Miner \| Orders) · portable ZIP |
| Debian/Ubuntu | `hackme-node_0.1.0-rc20_amd64.deb` or signed apt |
| Fedora/RHEL | `hackme-node-0.1.0-rc20.x86_64.rpm` |
| Arch | `hackme-node-0.1.0-rc20-x86_64.pkg.tar.zst` |
| Any Linux | `hackme_0.1.0-rc20_linux.tar.gz` |
| HackMe OS | `HackMe-OS-0.1.0-rc20-amd64.iso` |
| Fuzzing CLI | `hackme-fuzzing-0.1.0-rc20-{linux,windows}-amd64` (+ `-build`) |

Verify `SHA256SUMS.txt` / `SHA256SUMS-iso.txt` before install. Secrets never ship in the package — keys and `env.orders` live in the user home (`chmod 600`).

## Upgrade

- Downloads / self-update / `apt install hackme-node`
- Orders: Windows → Orders profile · Linux → `bash start_hackme_orders.sh`
- See [LINUX_PACKAGES.md](../LINUX_PACKAGES.md)

Economics v2 rules are unchanged (live since block 550,000; first shipped in 0.1.0-rc19).
