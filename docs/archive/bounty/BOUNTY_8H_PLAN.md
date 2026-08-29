# 8-Hour Bounty Marathon — Plan

**Started:** autopilot + `start_bounty_8h_marathon.sh` (nohup)  
**When back:** read **`docs/BOUNTY_8H_STATUS.md`** (auto-generated at end)

## Timeline (~8h)

| Wave | ~Time | What gets fuzzed |
|------|-------|------------------|
| **1 Autopilot** | 0–1.5h | OSS CVE×2, tokenize ultra, kleidi/arcadia/silo/0xmarkets, lowtier, Immunefi WASM×7, wormhole native, discovery |
| **2 Discovery** | 1.5–2.5h | moonwell, euler, morpho, reserve, angle, compound, hackenproof-public repos |
| **3 OSS CVE bulk** | 2.5–4.5h | **6 parsers** (25k iter, 4min each) — rotation minus HOLD |
| **4 Overnight loop** | 4.5–8h | **16 WASM** hedera/wormhole/berachain + **Foundry** kleidi/arcadia/silo rounds + wormhole 500k |
| **5 Tokenize deep** | if time left | 32k fuzz — HackenProof deadline **27.06** |
| **6 Report** | end | `BOUNTY_8H_STATUS.md` + rollup |

## One-liners when back

```bash
cat docs/BOUNTY_8H_STATUS.md
tail -50 logs/bounty-8h-marathon.nohup.log
cat reports/bounty/CURRENT_8H/rollup.json | jq .
bash scripts/ops/bounty_morning_report.sh reports/bounty/overnight/CURRENT
```

## Still manual (can't autofuzz)

- **darts RWA** — private repo, KYC
- **HackenProof signup** — dashboard.hackenproof.com
- **OSS disclosure** — centijson/libucl/cfgpack (no public repro)

## Re-run

```bash
DURATION_HOURS=8 bash scripts/ops/start_bounty_8h_marathon.sh
```
