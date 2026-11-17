# HackMe 0.1.0-rc21

**Channel:** `0.1.0-rc21` · **Date:** 2026-11-17 · **GitHub:** https://github.com/jokeez/hackme/releases/tag/0.1.0-rc21

## Highlights (since rc20)

- **Signed `latest.json`** — ed25519 detached signature (`latest.json.sig`) published with every cut; updater can verify (`HACKME_UPDATE_SIG_MODE`, default still `warn` until stable).
- **Security proxy harden** — Orders spendable / exchange price paths stay honest when the hub is set (no local-mirror fake budget).
- **Fail-closed Windows OpenCL / NVRTC / dirty tree** — release bundle refuses missing `workerpoh-opencl.exe`, missing NVRTC with CUDA, or a dirty / off-main tree.
- **Honest prepaid / paper D0 UI** — wallet chip labels paper ref mid (D0); hub prepaid source honored when hub URL is configured.
- **`stable_readiness_gate`** — structural checks always run in full local release; claiming stable still fails until money-path E2E (no soft pickup) + require-sig defaults land.
- **`RELEASE_MANIFEST` ISO honesty** — Docker ISO builds keep `hackme_os_iso: true` when the ISO is on disk.
- **CDN sums sync** — `sync_dist_cdn_sums.sh` keeps convenience digests current with the published channel.

Stable `0.1.0` is **not** ready yet (money-path E2E without soft pickup still open; L1 sig default remains `warn`).

## Artifacts

| Platform | Artifact |
|---|---|
| Windows | `HackMe-Setup-0.1.0-rc21.exe` (Miner \| Orders) · portable ZIP |
| Debian/Ubuntu | `hackme-node_0.1.0-rc21_amd64.deb` or signed apt |
| Fedora/RHEL | `hackme-node-0.1.0-rc21.x86_64.rpm` |
| Arch | `hackme-node-0.1.0-rc21-x86_64.pkg.tar.zst` |
| Any Linux | `hackme_0.1.0-rc21_linux.tar.gz` |
| HackMe OS | `HackMe-OS-0.1.0-rc21-amd64.iso` |
| Fuzzing CLI | `hackme-fuzzing-0.1.0-rc21-{linux,windows}-amd64` (+ `-build`) |
| Update channel | `latest.json` + `latest.json.sig` |

Verify `SHA256SUMS.txt` / `SHA256SUMS-iso.txt` before install. Secrets never ship in the package — keys and `env.orders` live in the user home (`chmod 600`).

## Upgrade

- Downloads / self-update / `apt install hackme-node`
- Orders: Windows → Orders profile · Linux → `bash start_hackme_orders.sh`
- See [LINUX_PACKAGES.md](../LINUX_PACKAGES.md) · blockers for stable: [HACKME_0.1.0_STABLE_BLOCKERS.md](./HACKME_0.1.0_STABLE_BLOCKERS.md)

Economics v2 rules are unchanged (live since block 550,000; first shipped in 0.1.0-rc19). Paid pool Dig/Hunt from rc20 remains.
