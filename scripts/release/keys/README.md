# HackMe L1 update signing (ed25519)

Signs `latest.json` for self-update (`update_hackme_miner.sh` / `.ps1`). Artifact digests remain SHA-256; this covers the **feed** so a CDN/GitHub swap cannot point miners at attacker archives without the private key.

## Files

| Path | Role |
|------|------|
| `hackme_update_ed25519.pub.pem` | **Public** verify key (committed, shipped with updaters) |
| `.secrets/hackme_update_ed25519.pem` | **Private** sign key (never commit; never print) |

## Keygen

```bash
bash scripts/release/keygen_update_ed25519.sh
# writes .secrets/hackme_update_ed25519.pem (0600) + refreshes this pub PEM
```

## Sign + publish

```bash
bash scripts/release/sign_latest_json.sh
# → dist/latest.json.sig (+ release_<VER>/latest.json.sig)
UPLOAD_GH=1 bash scripts/ops/publish_latest_json.sh   # uploads json + sig when present
# targeted CDN:
rsync -avz dist/latest.json dist/latest.json.sig hackme-vps:/opt/hackme/dist/
```

## Verify modes (updaters)

| `HACKME_UPDATE_SIG_MODE` | Behavior |
|---------------------------|----------|
| `warn` (default for rc) | Missing/invalid sig → WARN, continue (sha256 still required) |
| `require` | Missing/invalid sig → fail closed |
| `off` | Skip signature check |

Stable gate (`HACKME_CLAIM_STABLE=1`): L1 must verify ed25519 and default to **require** before tagging `0.1.0`.
