# HackMe 0.1.0-rc15 — B2B fuzz Phase 2 + pool anticheat

**Status:** development channel — Win/Linux/fuzz bundle aligned on **rc15** (ISO channel matches).

## Highlights

| Area | rc15 |
|------|------|
| **Mining dashboard** | New `#mining` fuzz banner — libheif OSS series archived CLEAN; customer B2B campaigns prioritized |
| **Fuzz product** | Wizard packs: `secrets`, `script_bounds`, `filter_utf8`, `parser_expat` · Scan/Audit/Deep tiers |
| **Pool anticheat** | Dynamic worker lease, exec cap 64 on hub, segment replay fail-closed, coverage bitmap @ 8192 |
| **What's new** | Dashboard notice rc15; stale libheif Day 1 messaging removed |

## Version alignment

- `main.go` · `scripts/release/CURRENT_VERSION` · `web/site/assets/app.js` · `dashboard.html` `DASHBOARD_UI_VER`

## Build

```bash
VERSION=0.1.0-rc15 bash scripts/release/make_release_bundle.sh
```

Previous channel: [HACKME_RC14.md](HACKME_RC14.md)
