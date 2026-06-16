# Day 7 PR Video Plan (Windows + Linux + Developers)

Goal: publish short, credible videos that convert viewers into miners or B2B integrators.

## 1) Windows miner install (60-90s)

- Show source of truth: `https://hackme.tech/downloads.html`.
- Download + run installer.
- Open dashboard at `http://127.0.0.1:8080`.
- Start mining from Start menu shortcut.
- Show live pool row and hashrate on `https://hackme.tech/pool/coordinator/api/pool/stats`.

Hook line:
"2 minutes from clean Windows to live pool worker."

## 2) Linux miner install (60-90s)

- Download tarball from Downloads page.
- Run `bash start_hackme_miner.sh`.
- Show dashboard + active worker session.
- Show the same worker reflected on pool stats endpoint.

Hook line:
"No script maze: one command, live worker in coordinator."

## 3) Developer B2B fuzzing flow (90-120s)

- Open `https://hackme.tech/developers.html`.
- On local node, issue token and run:
  - `hackme-fuzzing register --save`
  - `hackme-fuzzing wallet`
  - `hackme-fuzzing build ...`
  - `hackme-fuzzing create manifest.json`
- Show order on `#orders` and public read-only tracker.
- State clearly: remote `from_code` is blocked on hackme.tech by design.

Hook line:
"Build WASM locally, post order from localhost, pool fills work."

## 4) Credibility overlays (must include)

- "0 critical in latest CI on main"
- "Public stats API live"
- "Open source: github.com/jokeez/hackme"
- "Not Stratum: HTTP coordinator + workerpoh"
- "Off-chain accrual, on-chain settlement (operator runbook)"

## 5) Distribution plan

- X: 3-post thread per video (hook, demo, CTA).
- Telegram: one post per video + pinned recap weekly.
- Bitcointalk ANN: weekly long-form recap with links.

CTA for all channels:
"Start here: downloads + developers + contacts."

## 6) Tomorrow verdict checklist (before posting)

- `gh run list --limit 1` -> latest CI is green.
- `curl https://hackme.tech/pool/coordinator/api/pool/stats` -> status `ok`.
- `bash scripts/tests/fuzzing_cli_smoke.sh` -> PASS.
- Verify downloads links for both `hackme-fuzzing` and `hackme-fuzzing-build`.

