# External chain checkpoints (ops)

Daily signed checkpoint of tip + full ledger state, published to the public repo
`jokeez/hackme-checkpoints` and anchored in Bitcoin via OpenTimestamps. Format and the public
verification guide live in that repo's README; code is `cmd/checkpoint` + `internal/checkpoint`.

## Pieces on the hub

| Path | What |
|---|---|
| `/opt/hackme/bin/checkpoint` | built and shipped by `deploy_hackme_node.sh` |
| `/opt/hackme/scripts/ops/checkpoint_publish.sh` | create → `ots stamp` → `ots upgrade` older proofs → verify → commit/push |
| `hackme-checkpoint.{service,timer}` | daily 00:10 UTC, `User=hackme`; deploy enables the timer only once the clone exists |
| `/opt/hackme-checkpoints` | git clone of the public repo, owned by `hackme`, **outside** `/opt/hackme` so deploy `rsync --delete` never touches it |
| `/opt/hackme-checkpoints-venv/bin/ots` | `opentimestamps-client` in a venv |
| `~hackme/.ssh/hackme_checkpoints_deploy` | deploy key with write access to that one repo only |

`create` opens the chain DB read-only (`mode=ro`, `query_only`), never runs migrations, reads the
tip and all state in one read transaction (consistent WAL snapshot), refuses to extend a chain of
checkpoints that does not verify or was signed by another key, and refuses when the tip has not
advanced `CHECKPOINT_MIN_BLOCKS` (default 100) since the last checkpoint — a stalled chain makes
the unit fail visibly.

## One-time provisioning

```bash
# as root on the hub
sudo -u hackme mkdir -p /home/hackme/.ssh && chmod 700 /home/hackme/.ssh
sudo -u hackme ssh-keygen -t ed25519 -N '' -C hackme-checkpoints-deploy \
  -f /home/hackme/.ssh/hackme_checkpoints_deploy
# add the .pub as a *write* deploy key on jokeez/hackme-checkpoints (repo settings or gh)
python3 -m venv /opt/hackme-checkpoints-venv
/opt/hackme-checkpoints-venv/bin/pip install opentimestamps-client
install -d -o hackme -g hackme /opt/hackme-checkpoints
sudo -u hackme env GIT_SSH_COMMAND="ssh -i /home/hackme/.ssh/hackme_checkpoints_deploy -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new" \
  git clone git@github.com:jokeez/hackme-checkpoints.git /opt/hackme-checkpoints
systemctl enable --now hackme-checkpoint.timer
systemctl start hackme-checkpoint.service   # first checkpoint now
```

## When it fails

- `existing checkpoints do not verify` — someone edited the repo; never force-push over it, find out why.
- `node key … differs from checkpoint signer` — node identity changed; that is a chain-level event, not a checkpoint bug.
- `has not advanced … (chain stalled?)` — check block production first (watchdog `chain_stall`).

## Quick verify (operator)

```bash
systemctl list-timers hackme-checkpoint.timer --no-pager
journalctl -u hackme-checkpoint -n 30 --no-pager
ls -lt /opt/hackme-checkpoints/checkpoints | head
```

Public repo: https://github.com/jokeez/hackme-checkpoints
