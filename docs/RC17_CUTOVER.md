# rc17 cutover plan (local prep — deploy at cut only)

**Status:** PREP · bundle with D0 exchange + SUP wallet · **no early hub restart**

Related: [HACKME_RC16.md](HACKME_RC16.md) · [exchange D0 checklist](https://github.com/jokeez/hackme-exchange/blob/main/docs/D0_CHECKLIST.md) · [HUB_TAB](https://github.com/jokeez/hackme-exchange/blob/main/docs/HUB_TAB.md)

## What rc17 ships (target bundle)

| Area | Repo | Item |
|------|------|------|
| Hub `#exchange` | hackme | iframe → `https://exchange.hackme.tech/?embed=hub` |
| Hub CSP | hackme | `frame-src https://exchange.hackme.tech` |
| SUP wallet UI | hackme | `transfer_sup_v1`, activity, earnings |
| SUP public API | hackme | nginx allowlist: `/api/sup/tx/send`, `/api/sup/activity` |
| Paper SPA | hackme-exchange | D0 static + `frame-ancestors https://hackme.tech` |
| postMessage | hackme-exchange | `goto-tab` works from `hackme.tech` parent |

**Not in rc17:** public `exchange-api`, live custody, Postgres VPS, foreign CEX.

## Deploy order (single maintenance window)

1. **Static** — upload `hackme-exchange-d0-*.tar.gz` → `exchange.hackme.tech` path
2. **Smoke** — `curl -sI https://exchange.hackme.tech/` · open `/?embed=hub` CSP headers
3. **nginx** — `scripts/ops/nginx/hackme-site-domain.tls.conf` (SUP routes) → reload
4. **Node** — build hackme-node rc17 · restart **hackme.tech hub only** (not exchange host)
5. **Verify** — `bash scripts/ops/rc17_cutover_gate.sh` · hub `#exchange` · wallet SUP send

## Local gate (no deploy)

```bash
# HackMe
bash scripts/ops/rc17_cutover_gate.sh

# Exchange SPA (sibling)
cd ../hackme-exchange-demo && bash scripts/prepare_d0_static.sh

# Exchange API (private lab)
cd ../hackme-exchange-api && go test ./...
cd ../hackme-exchange-demo && npx tsx scripts/lab-smoke.ts   # API on :18443
```

## Rollback

| Layer | Rollback |
|-------|----------|
| Hub | redeploy rc16 binary + previous `dashboard.html` (iframe `:5199`) |
| Static | restore previous `dist-d0` tarball on mirror |
| nginx | revert site conf · `nginx -t && reload` |

## Until rc17

- **Production hub** — leave on rc16 runtime (code in git may be ahead; no restart)
- **D0 static** — can be prepared locally; DNS flip only in cut window
- **Lab dev** — `localStorage.setItem('hackme.exchange.origin', 'http://127.0.0.1:5199')`
