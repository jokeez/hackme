# L1 Crypto Stack v4 — live production fuzz

**Report:** https://hackme.tech/reports/l1-crypto-stack-v4.html

## What v4 adds over v3

| Layer | v3 | v4 |
|-------|----|----|
| Corpus | Official `qa-assets` offline probe | Same + **live useful-PoW fuzz** on hackme.tech |
| Payment | Static research | Real **HMC escrow** (20/80) from operator wallet |
| Workers | Local WASM only | **Pool workerfuzz** on public coordinator |
| Proof | HTML + repro JSON | HTML + per-campaign **fuzz_report_v2** links |

## Run (operator)

```bash
bash scripts/ops/run_l1_crypto_stack_v4_live.sh
DEPLOY=1 NODE_SSH=hackme-vps bash scripts/ops/run_l1_crypto_stack_v4_live.sh
```

Phases: `PHASE=offline|live|report|deploy|all`

Live campaigns are created on the **VPS internal node** (`127.0.0.1:18080`) — public nginx blocks `POST /api/fuzz/campaigns`.

## Honest scope

- Escrow debited from operator node wallet (`HMC-381c0c5e2cfcc714` on hub).
- Bounty/run payouts go to the **treasury worker** address derived from `.secrets/hackme_treasury_ed25519_seed.hex`.
- Not a claim of native Bitcoin Core libFuzzer differential (planned v4.1).
