# `scripts/` layout

| Path | In git | Purpose |
|------|--------|---------|
| [`build_*.sh`](build_security_task_pack.sh) | yes | WASM / task pack builds (CI) |
| [`check_invariants.sh`](check_invariants.sh) | yes | Chain invariants gate |
| [`lib/`](lib/) | yes | Shared shell helpers |
| [`release/`](release/) | yes | ISO / Windows / apt release pipeline |
| [`tests/`](tests/) | partial | CI gates (`run_daily.sh`, `run_ui_e2e.sh`, language pack) |
| [`ops/`](ops/) | partial | Production deploy, pool, settlement, bootstrap |
| [`vast/`](vast/) | **local only** | Vast.ai field-test helpers (gitignored) |

Lab scripts removed from the public index are listed in [`.gitignore`](.gitignore) — they remain on operator machines, not on [github.com/jokeez/hackme/tree/main/scripts](https://github.com/jokeez/hackme/tree/main/scripts).
