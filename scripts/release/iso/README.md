# HackMe Miner ISO build

See [docs/MINER_ISO.md](../../../docs/MINER_ISO.md) for operator/miner usage.

```bash
export HACKME_RELEASE_POOL_MINER_TOKEN="$(cat ../../.secrets/hackme_coordinator_worker_token)"
VERSION=0.1.0-rc11g bash build_hackme_miner_iso.sh
```

Files:

| File | Role |
|------|------|
| `build_hackme_miner_iso.sh` | Host entry (release tar + Docker) |
| `build_inner.sh` | debootstrap → squashfs → ISO |
| `Dockerfile` | Reproducible build image |
| `chroot-install.sh` | Packages + systemd inside chroot |
| `run-miner-worker.sh` | Pool worker (no Go on rig) |
| `overlay/` | `/etc/hackme`, systemd units |
