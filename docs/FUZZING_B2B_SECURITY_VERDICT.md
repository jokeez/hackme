# Fuzzing B2B — security verdict (May 2026)

## Decision

**Controlled launch model:** integrators use **hackme-fuzzing CLI** + **scoped `HACKME_DEVELOPER_TOKEN`**. The public site exposes a **read-only** status page only. Full mining dashboard and **admin token in the browser are rejected** for hackme.tech.

## Threats addressed

| Risk | Mitigation |
|------|------------|
| Admin token theft via XSS/phishing on static site | No token fields on hackme.tech; CLI stores token in env |
| Remote compiler abuse (`/api/tasks/from_code`) | HTTP **403** at nginx on public host; admin-only on loopback nodes |
| Fuzz API exposed to internet | `/api/fuzz/*` → **403** on hackme.tech except **GET** report/pulse/gate/csv (report token on node) |
| Customer WASM/manifest leakage | `GET /api/tasks` redacts `manifest_json` without developer/admin auth |
| Integrator over-privilege | Developer token: **only** `GET/POST /api/tasks` (not genesis, worker, tx/send, fuzz) |

## What remains public (by design)

- `GET /api/wallet` — treasury balance for escrow planning
- `GET /api/tasks` — redacted order summary (id, status, progress)
- [developer-console.html](https://hackme.tech/developer-console.html) — scoped token in session; WASM upload client-side
- Pool stats / explorer — same as miners

## Operator actions

1. Set `HACKME_DEVELOPER_TOKEN` on canonical node (see `scripts/ops/vps_ensure_developer_token.sh`).
2. Issue token to integrators over secure channel (not email/GitHub issues).
3. Deploy node + nginx: `deploy_hackme_node.sh`, `SYNC_NGINX_SITE_CONF=1 deploy_hackme_site.sh`.
4. Smoke: `fuzzing_public_hardening_smoke.sh`, `fuzzing_developer_portal_smoke.sh`.

## Verdict

**Acceptable for B2B fuzzing on hackme.tech** under this model. **Not acceptable** to restore browser admin console or public `from_code` on the canonical node.

## Related

- [DEVELOPERS_FUZZING.md](DEVELOPERS_FUZZING.md)
- [SECURITY_AUDIT_REDTEAM.md](SECURITY_AUDIT_REDTEAM.md)
