# Linux packages and Docker

Every release ships the same binaries in several wrappers. All of them install to `/opt/hackme`.
Verify downloads against `SHA256SUMS.txt` next to the files.

| System | File | Install |
|---|---|---|
| Debian / Ubuntu | `hackme-node_<ver>_amd64.deb` | `sudo apt install ./hackme-node_<ver>_amd64.deb` (or the signed apt repo) |
| Fedora / RHEL / openSUSE | `hackme-node-<ver>.x86_64.rpm` | `sudo dnf install ./hackme-node-<ver>.x86_64.rpm` |
| Arch / Manjaro | `hackme-node-<ver>-x86_64.pkg.tar.zst` | `sudo pacman -U hackme-node-<ver>-x86_64.pkg.tar.zst` |
| Any glibc Linux | `hackme_<ver>_linux.tar.gz` | `tar -xzf … && cd linux && bash start_hackme_miner.sh` |

Package versions sort pre-releases before the final release (`0.1.0~rc19` for rpm, `0.1.0rc19` for
pacman), so a later stable release upgrades cleanly.

## Docker (miner)

The image is built from the published Linux tarball (prebuilt CPU / CUDA / OpenCL workers) and
checks its SHA256 during the build.

```bash
docker run -d --name hackme --restart unless-stopped --stop-timeout 60 \
  --network host --gpus all \
  -v hackme-state:/state hackme/miner:<ver>
```

- `--network host` keeps the node on `127.0.0.1:8080` exactly like a native install (the dashboard is
  not exposed to the LAN). Set `-e HACKME_PORT=18080` if port 8080 is taken.
- `--gpus all` needs the NVIDIA container toolkit; omit it for CPU-only mining.
- Runs as uid 10001, never root.
- **`/state` holds the wallet key** (`data/node_ed25519.seed`), the chain DB, `.env` and logs.
  Back the volume up — losing it loses the wallet:
  `docker run --rm -v hackme-state:/s -v "$PWD":/b debian tar -czf /b/hackme-state.tgz -C /s .`
- Upgrade: `docker pull` the new tag, `docker stop -t 60 hackme && docker rm hackme`, run again with the same volume (`rm -f` kills the node without flushing).
- Hunt (ASAN/bwrap) needs unprivileged user namespaces, which Docker blocks by default: in the
  container the worker runs Dig + PoH and soft-skips Hunt.

Build locally from a release dir: `VERSION=<ver> bash scripts/release/docker/build_miner_image.sh`
(`DOCKER_BUILD_NETWORK=host` if the default bridge has no DNS).
