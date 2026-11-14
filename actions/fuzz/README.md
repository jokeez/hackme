# hackme-fuzz-action

GitHub Action for HackMe **Dig**: on every PR it builds your guard wasm, runs
it through the **same sandbox gate the pool uses** (`sandbox.ValidateCheckWasm`)
and posts what the Dig order would look like — preset, guard pack, package,
budget, wasm hash — as a sticky PR comment and in the job summary.

> **Status: dry-run MVP.** Nothing is sent to the pool and no secrets are
> needed, so it is safe on PRs from forks. Real publish (prepaid / org escrow
> or a scoped publish token) comes later — see [Roadmap](#roadmap).
> Hunt is intentionally **not** run per PR (too slow); it will be opt-in.

## Usage

```yaml
# .github/workflows/hackme-dig.yml
name: hackme-dig
on: pull_request

permissions:
  contents: read

jobs:
  dig:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      pull-requests: write   # sticky comment (read-only on fork PRs → just a warning)
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: jokeez/hackme/actions/fuzz@<commit-sha>
        with:
          source: fuzz/guard.rs     # or: wasm: build/guard.wasm
          language: rust
          guard-pack: overflow_smoke
          package: scan
```

Just check that the pipeline works with a shipped guard:

```yaml
      - uses: jokeez/hackme/actions/fuzz@<commit-sha>
        with:
          dig-preset: scan_smoke
```

Pin the action to a commit SHA, not a branch.

**Do not** run it from `pull_request_target`: that event runs with a
write token against untrusted PR code. Plain `pull_request` is all it needs.

## Gate mode (phase 2, preview)

Wait for a campaign that is already published (by an Orders node, a person,
a separate job) and turn its CI gate into the job result:

```yaml
on: workflow_dispatch   # secrets are not available to fork PRs anyway
jobs:
  gate:
    runs-on: ubuntu-24.04
    steps:
      - uses: jokeez/hackme/actions/fuzz@<commit-sha>
        with:
          mode: gate
          hackme-url: https://your-orders-node.example
          campaign-id: campaign-audit-20261113t...
          report-token: ${{ secrets.HACKME_REPORT_TOKEN }}
          gate-timeout: 45m
```

It polls `pulse` until the campaign is `completed` / `cancelled` / `failed`,
then reads `/gate` with `max_critical` / `max_high` and `min_runs_done`
(default: the full budget). A campaign that is still running never counts
as a pass — it has no findings yet, so its gate would pass trivially. The
report token only reaches the engine through env; URLs must be https
(http only for loopback) and redirects are refused.

Outputs: `gate-pass`, `campaign-status`, `runs-done`, `critical-count`,
`high-count`. The verdict gets its own sticky comment, separate from the
dry-run plan.

## Publish mode (phase 2, preview)

Creates a real, capped Dig order on **your Orders node** and waits for its
gate. It needs the node's scoped CI token (`HACKME_CI_TOKEN`, endpoint
`POST /api/ci/dig`, see [docs/API.md](../../docs/API.md)) — never the fleet
publish token or the node admin token.

```yaml
on: workflow_dispatch
jobs:
  dig:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: jokeez/hackme/actions/fuzz@<commit-sha>
        with:
          mode: publish
          dig-preset: overflow_smoke
          hackme-url: https://your-orders-node.example
          ci-token: ${{ secrets.HACKME_CI_TOKEN }}
          gate-timeout: 45m
```

- Runs only on `workflow_dispatch`, a push to a **protected** branch, or a
  same-repo PR carrying `publish-label`. Fork PRs, `pull_request_target`
  and everything else are refused before any network call.
- The wasm goes through the same pool gate as a dry-run first; a rejected
  wasm is never sent.
- One POST, no retries (it spends escrow). The node picks the package
  budget and enforces hourly/daily/HMC caps.
- By default the node runs the order as a **local Dig** (no pool, no PoH
  order) for every package. Only an operator with `HACKME_CI_ALLOW_POOL=1`
  sends CI orders to the pool, and then prepaid `hubv1` +
  `escrow_fingerprint` apply like on any Orders publish. Start with
  `scan` / `overflow_smoke`.
- The campaign's report token stays inside the engine process — it is
  never written to outputs or logs. Output: `campaign-id` + the gate outputs.

## Inputs

| Input | Default | |
|---|---|---|
| `wasm` | | Prebuilt guard `.wasm`, relative to the workspace. |
| `source` + `language` | | Single-file guard to compile: `rust` · `c` · `cpp`. |
| `dig-preset` | | Shipped guard: `scan_smoke` `overflow_smoke` `bounds_smoke` `state_smoke` `script_bounds` `secrets` `filter_utf8` `parser_expat`. Can't be combined with `wasm`/`source`. |
| `guard-pack` | | Pack config (input mode, seeds, budgets) applied to your wasm, or run as-is. |
| `package` | preset/pack default, else `scan` | `scan` · `audit` · `deep`. |
| `title` | | Campaign title (control chars stripped, 120 chars max). |
| `mode` | `dry-run` | `dry-run`, `gate` or `publish` (see above). |
| `comment` | `true` | Keep a sticky PR comment updated. |
| `fail-on-reject` | `true` | Fail the job when the gate rejects the wasm or it doesn't build. |
| `hackme-url` | | Gate/publish: node base URL (https). |
| `ci-token` | | Publish: scoped Orders CI token (from a secret). |
| `publish-label` | | Publish: PR label that opts a same-repo PR in. |
| `campaign-id` / `report-token` | | Gate: campaign to wait for and its report token. |
| `gate-timeout` | `30m` | Gate/publish: max wait for the campaign to finish (≤ 6h). |
| `max-critical` / `max-high` | `0` | Gate/publish: allowed crash-class findings. |
| `min-runs-done` | `-1` | Gate/publish: required runs, `-1` = full budget. |
| `github-token` | `github.token` | Token for the comment. |

Outputs: `valid`, `source`, `dig-preset`, `guard-pack`, `dig-package`,
`wasm-sha256`, `wasm-bytes`, `plan-path` (plan JSON — the exact
`POST /api/security-audit` body, wasm referenced by hash).

## Writing a guard

WASM ABI v1 ([docs/TASK_LANGUAGES.md](../../docs/TASK_LANGUAGES.md)): export
`check(i64) -> i32` (or `check_bytes(ptr, len) -> i32` + memory for byte
packs), return `1` when the input hits what you are hunting for. Keep Rust
guards `#![no_std]` — the pool cap is **64 KiB**. Example:
[examples/guard.rs](examples/guard.rs).

Build flags match `scripts/build_task_wasm.sh` (Rust additionally gets
`-C strip=symbols -C panic=abort`).

## Security notes

- Every input reaches the scripts through `env`, never `${{ }}` inside `run:`.
- `wasm` / `source` must resolve (symlinks included) inside `$GITHUB_WORKSPACE`;
  the wasm is read with the pool size cap.
- The compiler is exec'd with fixed arguments, no shell.
- The comment only ever edits a comment written by the same identity with the
  marker on its first line; user-controlled text is rendered in neutralised
  code spans.
- Third-party actions are pinned by SHA.

## Engine

The logic lives in [`cmd/hackme-fuzz-action`](../../cmd/hackme-fuzz-action) and
runs locally too:

```sh
go run ./cmd/hackme-fuzz-action -dig-preset scan_smoke
go run ./cmd/hackme-fuzz-action -workspace . -source actions/fuzz/examples/guard.rs -language rust
```

Exit codes: `0` ok · `1` rejected by the gate / build failed · `2` bad inputs.

## Roadmap

1. ✅ Dry-run: build, gate, plan, PR comment.
2. 🚧 Publish behind an explicit opt-in through the Orders node's scoped CI
   token; prepaid `hubv1:` + `escrow_fingerprint` on the pool path — never
   the shared fleet publish token in a public repo (`mode: publish`, preview).
3. 🚧 Poll `GET /api/fuzz/campaigns/{id}/gate` and turn `pass` / `reasons[]`
   into the job verdict (`mode: gate`, preview).
4. Hunt via `workflow_dispatch` or a PR label, not on every push.
