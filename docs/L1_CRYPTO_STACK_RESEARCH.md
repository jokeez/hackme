# L1 Crypto Stack — upstream-ported guards

**Report:** https://hackme.tech/reports/l1-crypto-stack.html

## What changed (v2)

Guards are **ported C excerpts** from named upstream functions, not synthetic one-liners:

| Chain | Upstream | Ported logic |
|-------|----------|--------------|
| Bitcoin | `script.cpp` | `GetScriptOp` + `MAX_SCRIPT_ELEMENT_SIZE` (520) |
| Ethereum | `state_transition.go` | `uint256.FromBig` overflow class (carry probe) |
| Dogecoin | `script.cpp` | `CScript::HasValidOps()` |
| Litecoin | `script.cpp` | `GetScriptOp` (fork family) |
| HackMe | `order_tasks.go` | `InsertOrderTask` + `MinOrderPrepaidHMC` / difficulty |

Sources: `tasks/sources/security/upstream/*.c` — see `ATTRIBUTION.md`.

## Regenerate

```bash
bash scripts/build_upstream_l1_pack.sh
bash scripts/ops/run_l1_crypto_stack_research.sh
bash scripts/ops/l1_crypto_stack_gate.sh
bash scripts/ops/deploy_hackme_site.sh   # publish HTML
```

## Honest scope

- ✅ Ported logic from real files (links in report)
- ✅ Same HackMe WASM sandbox as Security Audit / useful-PoW
- ❌ Not running `bitcoind` / `geth` / full upstream test suites
- ❌ Not claiming new CVEs without maintainer repro

Companion: [Bitcoin Core 5-module](https://hackme.tech/reports/bitcoin-core-5module.html).
