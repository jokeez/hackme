# Security research campaign #1 (operator plan)

**Public write-up (screenshots, code, reproduce):** [security-note-01/README.md](security-note-01/README.md)

Honest public story: **useful-PoW finds violation-class inputs** in WASM guards — not “we hacked Bitcoin Core.”

## What to run first (recommended)

| Priority | Target | Why | Effort |
|----------|--------|-----|--------|
| **1 — now** | `script_push_bounds_guard` (Rust/C++) | Bitcoin Script **520-byte push** rule, simplified; great for BCT/TG | 1 day |
| 2 | Existing pack: `overflow_guard`, `bounds_guard`, `state_transition_guard` | Already in repo; multi-language proof | hours |
| 3 | Small OSS (not Core) | `rust-miniscript`, `rust-bitcoin` **script** subset as new WASM | 1–2 weeks |
| 4 | Mining-adjacent C | Old stratum proxy / pool utils (MIT) — bounds, parse fuzz | 2+ weeks |
| **Avoid** | Bitcoin Core monolith | OSS-Fuzz, disclosure politics, wrong tool fit | — |

## Campaign #1 — script push bounds (Bitcoin-inspired)

**Story:** Distributed search on HackMe finds nonces where a checker flags **OP_PUSHDATA1 + length > 520** (consensus-style violation class).

**Build:**

```bash
bash scripts/build_security_task_pack.sh
bash scripts/ops/run_security_research_campaign.sh
```

**Submit to prod** (treasury must cover escrow):

```bash
hackme-fuzzing register --save   # once
export HACKME_FUZZING_BASE=https://hackme.tech
export HACKME_DEVELOPER_TOKEN="$(cat ~/.config/hackme/developer.token)"

TASK=script_push_bounds_guard BUILD_LANG=rust \
DEV_TOKEN="$HACKME_DEVELOPER_TOKEN" BASE=https://hackme.tech \
  bash scripts/ops/run_security_research_campaign.sh --submit
```

**Screenshots for post:** order on `developers.html` / `GET /api/tasks` with token, `progress_count`, pool stats, one line from `tasks/sources/security/rust_script_push_bounds_guard.rs`.

**Wording (safe):**

- ✅ “Consensus-**inspired** guard; violation class OP_PUSHDATA1 / 520 B”
- ✅ “Found by useful-PoW on HackMe (order id …)”
- ❌ “CVE in Bitcoin Core” / “RCE in BTC” without maintainer coordination

## Posts

- Announce: https://hackme.tech/news.html · https://t.me/hackme_tech

## Phase 2 targets (pick one after #1)

1. **miniscript** — compile a tiny parser invariant to WASM (real OSS, smaller than Core).
2. **Base58Check decoder** — classic off-by-one / overflow guards (mining wallet tooling angle).
3. **Public CVE reproduction** — fixed bug in library X (educational, cite CVE + fixed version).

## Legal / ethics

- No full exploit drop before vendor patch on real projects.
- AGPL source + reproducible manifest + order id in every public claim.
