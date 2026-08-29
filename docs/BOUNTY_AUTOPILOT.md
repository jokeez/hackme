# Bounty Autopilot

Autonomous multi-project fuzz — one command, all tracks.

## Run

```bash
bash scripts/ops/run_bounty_autopilot.sh
FAST=1 bash scripts/ops/run_bounty_autopilot.sh   # half budgets, ~2h
SKIP_PHASES=immunefi_wasm,native_wormhole bash scripts/ops/run_bounty_autopilot.sh
```

Report: `reports/bounty/CURRENT_AUTOPILOT/rollup.json`

## Phases (sequential)

| Phase | What |
|-------|------|
| `oss_cve` | 2 parsers/night from rotation (ASAN upstream) |
| `tokenize_ultra` | HackenProof pin `52b0322` + 16k fuzz *(program closed 2026-06-27 — skip via `SKIP_PHASES=tokenize_ultra`)* |
| `foundry_open` | tokenize, kleidi, arcadia, silo, 0xmarkets |
| `hackenproof_lowtier` | Arcadia + Silo Foundry max |
| `immunefi_wasm` | Hedera + Wormhole + Berachain WASM guards |
| `native_wormhole` | Go VAA Unmarshal probe |

Registry: `upstream/bounty_fuzz_registry.json`

## VPS timer

```bash
sudo cp scripts/ops/systemd/hackme-bounty-autopilot.* /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now hackme-bounty-autopilot.timer
```

Daily @ 03:30 UTC.

## Add projects

Edit `upstream/bounty_fuzz_registry.json` → `foundry_open[]` with public repo + `test_match`.

Closed repos (darts RWA) stay in `closed_need_access` until HackenProof grants repo.

## Manual steps / disclosure

- OSS upstream issues: [OSS_DISCLOSURE_ACTIONS.md](OSS_DISCLOSURE_ACTIONS.md) · per-project drafts in repo root `docs/OSS_CVE_DISCLOSURE_*.md`
- Historical marathon notes: [archive/bounty/README.md](archive/bounty/README.md)
