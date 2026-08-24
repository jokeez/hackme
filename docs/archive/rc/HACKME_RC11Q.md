# HackMe 0.1.0-rc11q — superseded by rc11s

> **Historical.** Current download channel: [HACKME_RC15.md](../../HACKME_RC15.md) (`0.1.0-rc15`). Intermediate: [HACKME_RC12W.md](../../HACKME_RC12W.md).

## What rc11q shipped

- Settlement API fix on public hub (`/api/worker/settlement` timeout)
- Live upgrade banner via `miner-notices.json`
- ISO aligned with Win/Linux tag
- `main.go` split (~800 lines into focused modules)

## Why upgrade to rc11s

- Linux miners: `worker_script_missing` on fresh tarball (layout bug) — **fixed in rc11s**
- Fuzz pool settle outbox + escrow cleanup + nginx routes
- Mining settlement timer ops fix on VPS

Artifacts remain at `https://hackme.tech/dist/release_0.1.0-rc11q/` for audit; new installs should use **rc11s**.
