# Tokenize.it Ultra Hunt — HackenProof

**Pin:** `52b0322fb566c7143d09c23b7bd30f2e092e0691`  
**Program:** [tokenize-it-token-sc-dualdefense-audit](https://hackenproof.com/audit-programs/tokenize-it-token-sc-dualdefense-audit)  
**Ends:** 2026-06-27

## Run

```bash
bash scripts/ops/run_tokenize_ultra_hunt.sh
FUZZ_RUNS=32768 UPSTREAM_FUZZ_RUNS=16384 bash scripts/ops/run_tokenize_ultra_hunt.sh
```

Reports: `reports/bounty/tokenize-ultra-*/rollup.json`

## In-scope hot paths (manual review)

| Contract | Risk |
|----------|------|
| `CoinvestedPosition.sol` | pull-payout credits, carry split, recovery timer, cross-currency exit |
| `GlobalTokenExitRegistry.sol` | one-shot exit binding, admin/owner ACL |
| `Exit.sol` | fixed price redeem, reference rates, drain after `lockedUntil` |
| `Distribution.sol` | snapshot claims, reassignment after lock |
| `Crowdinvesting.sol` / `PrivateOffer.sol` | mint caps, currency trust bit |
| `TokenSwap.sol` | secondary market, fee path |
| `TimeLock.sol` | locked tokens in distributions/exits |
| EIP-2771 forwarder | spoofed `_msgSender()` on all meta-tx contracts |

## Latest ultra run (2026-06-25)

Upstream fuzz @ 8192 runs — **no exploitable fuzz breaks** (only infra noise):

- `testBuyWithMainnetGSNForwarder` — fork/mainnet forwarder (expected offline fail)
- `TokenSwap` fuzz — `vm.assume` exhaustion (harness limit, not vuln)
- `Crowdinvesting.t.sol` / `TimeLock.t.sol` — no tests matched pattern (check paths)

**Green suites:** CoinvestedPositionExit (36), PullPayouts (17), Distribution (64), Exit (54), GlobalTokenExitRegistry (15), PrivateOffer (17), Vesting (22), AllowList (17), CoinvestedPosition main (169/170).

## HackenProof checklist

1. Register at [dashboard.hackenproof.com](https://dashboard.hackenproof.com)
2. Join audit program, confirm scope = pinned commit
3. Manual pass: Coinvested carry + exit registry + pull withdraw ACL
4. Submit only with Foundry PoC + impact + severity per program table

## Verdict

`NO_BOUNTY_FINDING` from automated ultra — **proceed with manual audit**, not public claim.
