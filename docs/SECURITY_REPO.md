# Repository hygiene — what must never reach GitHub

Before `git push` and when reviewing PRs, keep secrets out of history.

## Never commit

| Item | Where it lives | Why |
|------|----------------|--------|
| Coordinator **admin** token | `.secrets/hackme_coordinator_admin_token`, VPS `/opt/hackme/.env.coord` | Full pool control |
| Coordinator **worker** token (if treated as secret) | `.secrets/hackme_coordinator_worker_token` | Claim/submit as your fleet |
| Desktop **admin** token | `.env.desktop`, `HACKME_ADMIN_TOKEN` | Local node mutating API |
| Miner **Ed25519 seed** | `data/*seed*`, `HACKME_MINER_ED25519_SEED_HEX` in env | Signs payouts |
| Telegram bot token | `.env.newsbot`, `telegram_bot.env` | Bot impersonation |
| SSH passwords | env `VPS_SSH_PASSWORD` only at runtime | `deploy_vps_miner_password.py` reads env — never hardcode |
| SQLite / chain DB | `data/`, `logs/desktop/data/` | Wallets and local state |
| Logs, reports, ISO builds | `logs/`, `reports/`, `dist/` | Noise + accidental PII |

All of the above are listed in [`.gitignore`](../.gitignore).

## Safe to commit

- `*.example` env templates with `REPLACE_…` / `change-me-…` placeholders  
- `scripts/tests/coordinator_stress.env` — **local stress only**, fake token `stress-coord-admin-token`  
- Public URLs: `https://hackme.tech`, pool paths, worker id **examples** (`worker-kapa-pc`)  
- Documentation without real tokens, seeds, or operator passwords  

## Quick scan before push

```bash
git status
git diff --cached | grep -iE 'HMC-[0-9a-f]{16}|ghp_|sk-|BEGIN (RSA|OPENSSH)|COORD.*=[a-f0-9]{32}' && echo "REVIEW REQUIRED" || echo "no obvious secrets in staged diff"
```

If anything sensitive was committed: rotate tokens on VPS, rewrite history only if you know the impact, and force operators to redeploy.

## GitHub / public site

- Releases: binaries via [hackme.tech/downloads](https://hackme.tech/downloads.html) + GitHub Releases — not committed `dist/` blobs.  
- Issue templates: no paste of `.env` or coordinator admin token.  
- Use [contacts.html](https://hackme.tech/contacts.html) for security reports — not public exploits in issues.

See also [SECURITY.md](SECURITY.md) (runtime model).
