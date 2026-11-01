# `scripts/` layout

| Path | In git | Purpose |
|------|--------|---------|
| [`build_*.sh`](build_security_task_pack.sh) | yes | WASM / task pack builds (CI) |
| [`check_invariants.sh`](check_invariants.sh) | yes | Chain invariants gate |
| [`lib/`](lib/) | yes | Shared shell helpers |
| [`release/`](release/) | yes | ISO / Windows / apt release pipeline |
| [`tests/`](tests/) | partial | CI gates (`run_daily.sh`, `run_ui_e2e.sh`, language pack) |
| [`ops/`](ops/) | yes | Production deploy, pool, settlement, bootstrap customer |
| `archive/` | **local only** | Spent one-shot ops helpers (gitignored — not on GitHub) |
| [`vast/`](vast/) | **local only** | Vast.ai field-test helpers (gitignored) |
| [`local/`](local/) | **local only** | Lab / marathon scripts (gitignored — not on GitHub) |
| `reports/` | **local only** | Operator session dumps (gitignored) |

See [`ops/README.md`](ops/README.md) for the supported production script list.
