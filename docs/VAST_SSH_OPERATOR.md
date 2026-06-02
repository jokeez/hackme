# Vast GPU tests — SSH (never commit secrets)

All credentials live under **`.secrets/vast/`** (gitignored).

## One-time setup

```bash
mkdir -p .secrets/vast
chmod 700 .secrets/vast

# Private key for Vast instances (paste from Vast UI — do NOT commit)
nano .secrets/vast/id_ed25519_vast
chmod 600 .secrets/vast/id_ed25519_vast

# Instance list (copy template below)
cp docs/vast-instances.example.json .secrets/vast/instances.json
chmod 600 .secrets/vast/instances.json
```

Pack path (local): `dist/vast-gpu-matrix-*.tar.gz` — also gitignored under `dist/`.

## `instances.json` template

See [vast-instances.example.json](vast-instances.example.json) — copy to `.secrets/vast/instances.json`.

Fields per GPU session:

| Field | Example |
|-------|---------|
| `name` | `rtx5090-01` |
| `host` | IP or hostname from Vast |
| `port` | SSH port |
| `user` | `root` |
| `worker_id` | `vast-5090-01` |
| `ssh_key` | absolute path to private key |
| `run_seconds` | `3600` |
| `fleet` | `false` or `true` for 6-GPU box |

## Run from your PC (agent or you)

```bash
# One instance by name:
bash scripts/vast/ssh_run_session.sh rtx5090-01

# Full matrix from instances.json:
bash scripts/vast/ssh_run_matrix.sh

# UI check only (coordinator snapshot, no SSH):
bash scripts/vast/ssh_check_ui.sh vast-5090-01
```

Logs: `reports/vast-remote/` (gitignored).

## Do not commit

- `.secrets/vast/*` (keys, `instances.json`)
- `env.vast` (worker token + seed)
- `dist/vast-gpu-matrix-*.tar.gz`
- `reports/vast-*`

## Giving access to Cursor agent

Paste in chat only:

- `name`, `host`, `port`, `user` (no private key in chat if possible)
- Or say: «ключ лежит в `.secrets/vast/id_ed25519_vast`, instances.json обновлён»

Never paste `COORD_TOKEN` or `HACKME_MINER_ED25519_SEED_HEX` in issues/PRs.
