# exchange.hackme.tech — framing CSP (hub iframe)

**Origin** is Caddy on `89.150.41.40` (`scripts/ops/caddy/exchange.Caddyfile`), not the
legacy nginx sketch in `scripts/ops/nginx/hackme-exchange-domain.tls.conf`.

**Mining hub (`132.243…`):** do **not** enable `hackme-exchange-domain.conf` in
`sites-enabled`. A hard-coded `proxy_pass https://hackme.tech` fails `nginx -t` /
start when DNS flaps and takes down pool/coordinator with it. Paper SPA + public
desk API live only on the exchange origin VPS. If a local sketch is required,
use `resolver` + variable `proxy_pass` (see the reference conf) — still prefer
keeping the site disabled.

Required HTTP response headers (browsers ignore `frame-ancestors` in `<meta>`):

- `Content-Security-Policy` with
  `frame-ancestors 'self' https://hackme.tech http://127.0.0.1:8080 http://localhost:8080`
  and **tight** `connect-src 'self' https://hackme.tech` only
  (desk API is same-origin `/desk-api`; BTC mark is `/mark-proxy/btc` — **no**
  `exchange-api.hackme.tech` / `api.binance.com` / Cloudflare Insights in CSP)
- `Cross-Origin-Resource-Policy: cross-origin`
- **No** `X-Frame-Options: SAMEORIGIN` (use `-X-Frame-Options` in Caddy)

## Status (2026-10-06)

Origin Caddy CSP hardened: removed cross-origin API + Binance + CF Insights from
`connect-src` / `script-src`. Browser desk traffic stays on `/desk-api` +
`/hub-proxy` + `/pool-proxy` + `/mark-proxy/btc`.

`npm run smoke:live` (paper SPA) asserts HTTP CSP + no `XFO: SAMEORIGIN`.

## If edge regresses

1. On origin: `caddy validate --config /etc/caddy/Caddyfile && systemctl reload caddy`
2. Confirm origin-direct:
   ```bash
   curl -skI --resolve exchange.hackme.tech:443:89.150.41.40 https://exchange.hackme.tech/ \
     | grep -iE 'content-security|x-frame|cross-origin-resource'
   ```
3. Confirm CSP does **not** list `exchange-api` / `binance` / `cloudflareinsights`:
   ```bash
   curl -sI https://exchange.hackme.tech/ | tr ',' '\n' | grep connect-src
   ```
4. If origin is good but CF edge still injects `X-Frame-Options: SAMEORIGIN`, use
   Cloudflare → Rules → Transform Rules → Modify Response Header for
   `exchange.hackme.tech`: remove `X-Frame-Options`, set CSP / CORP as above, purge cache.

Do **not** assume CF is the only source of XFO — the regression that blocked hub
embed was origin Caddy shipping `X-Frame-Options: SAMEORIGIN` without CSP.
