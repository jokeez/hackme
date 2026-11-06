# HackMe 0.1.0-rc19

## Highlights
- **Longer wallets + Economics v2 prep** — channel prepares address lengthening and the Economics v2 switch (not activated; live payouts unchanged until the announce height).
- **Miner + Orders packages** — same binaries as `.deb` (Ubuntu/Debian), `.rpm` (Fedora/RHEL), Arch `pkg.tar.zst`, Linux tarball, Windows Setup/ZIP, HackMe OS ISO, and Docker.
- **Fedora / slim fixes** — admin token generation falls back to `/dev/urandom` when openssl/python are missing; packages require `bash`, `curl`, `ca-certificates`.
- **Orders beside Miner** — clear error if port 8080 is taken; `HACKME_ORDERS_PORT=8081` runs Orders next to a miner and remembers the port.
- **Docker Orders** — `-e HACKME_PROFILE=orders` (state in `/state`); zig/from_code toolchain download off by default so first boot does not hang.
- **Downloads picker** — Miner vs Orders role switch with OS-specific installers (deb/rpm/Arch/tar/ISO/Docker).

## Secrets
- Packages never ship `.env` or pool tokens under `/opt/hackme`.
- Keys and `env.orders` live in the user home (`chmod 600`).

## Install
Verify `SHA256SUMS.txt` / `SHA256SUMS-iso.txt` before run. See [docs/LINUX_PACKAGES.md](../LINUX_PACKAGES.md).

## Known pre-1.0
- Self-update is sha256-only (not signed). Verify release assets before install.
