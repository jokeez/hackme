# HackMe Developers — Fuzzing (orders) without mining UI

**Audience:** B2B clients, security vendors, integrators — not GPU miners.

| Need | Use |
|------|-----|
| Mine HMC on a rig | [downloads.html](https://hackme.tech/downloads.html) · ISO / Windows worker |
| Pay for fuzzing work | **hackme-fuzzing CLI** + scoped developer token |

## Integrator token (automatic)

No support ticket required on hackme.tech when `HACKME_INTEGRATOR_SELF_REGISTER=1` (default).

| Channel | Command / URL |
|---------|----------------|
| Web | [developers.html#integrator-token](https://hackme.tech/developers.html#integrator-token) — **Issue new token** |
| CLI | `hackme-fuzzing register --save` |
| HTTP | `POST /api/integrator/register` → `{ developer_token }` (once) |
| Rotate | `POST /api/integrator/rotate` or `hackme-fuzzing rotate --save` |

Token file (CLI `--save`): `~/.config/hackme/developer.token` (mode 600).

## Security model (hackme.tech)

- **No admin token** on public pages — [fuzzing-console.html](https://hackme.tech/fuzzing-console.html) is read-only.
- **`/api/tasks/from_code`** and **`/api/fuzz/*`** return HTTP 403 on the public edge.
- WASM built **locally** (`hackme-fuzzing build`); manifest uses `wasm_check_hex`.

See [FUZZING_B2B_SECURITY_VERDICT.md](FUZZING_B2B_SECURITY_VERDICT.md).

## CLI workflow

```bash
hackme-fuzzing register --save
hackme-fuzzing build -lang rust -source check.rs -out ./fuzzing-out \
  -id acme-audit-001 -reward 0.01 -difficulty 5 -target 3
hackme-fuzzing create ./fuzzing-out/acme-audit-001.manifest.json
hackme-fuzzing tasks
hackme-fuzzing rotate --save   # invalidates previous token
```

Downloads: https://hackme.tech/downloads.html#fuzzing-client

## HTTP API

| Method | Path | Auth |
|--------|------|------|
| GET | `/api/integrator/status` | Public |
| POST | `/api/integrator/register` | Public (rate limited) |
| POST | `/api/integrator/rotate` | Current developer token |
| GET | `/api/wallet` | Public |
| GET | `/api/tasks` | Public summary; full with developer token |
| POST | `/api/tasks` | `X-Hackme-Developer-Token` |
| POST | `/api/tasks/from_code` | **403** on hackme.tech |

## Order manifest

Prefer `wasm_check_hex` from local build (no server-side compiler):

```json
{
  "id": "acme-audit-001",
  "kind": "synthetic_poh_v1",
  "difficulty_score": 10,
  "reward_hmc": 0.01,
  "target_solves": 5,
  "payer_ref": "acme:project-42",
  "artifact_hash": "<sha256 of wasm>",
  "wasm_check_hex": "<hex-encoded wasm module>"
}
```

## Operator deploy

```bash
NODE_SSH=hackme-vps bash scripts/ops/deploy_fuzzing_b2b_release.sh
```

Or step-by-step: `deploy_hackme_node.sh`, `vps_ensure_integrator_env.sh`, `SYNC_NGINX_SITE_CONF=1 deploy_hackme_site.sh`.

Env: `HACKME_INTEGRATOR_SELF_REGISTER=1`, `HACKME_INTEGRATOR_MAX_TOKENS=200`, optional legacy `HACKME_DEVELOPER_TOKEN`.

## Verify

```bash
bash scripts/tests/integrator_self_service_smoke.sh
bash scripts/tests/fuzzing_public_hardening_smoke.sh
bash scripts/tests/fuzzing_developer_portal_smoke.sh
```

## Self-hosted node

Full dashboard + `from_code` on loopback: `scripts/ops/desktop_mode_up.sh` — **not** exposed on hackme.tech.
