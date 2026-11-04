# Economics v2 — launch runbook

Operator checklist from "pick the height" to "first v2 block is healthy". Code is on `main`
and inert until a height is set. Read the whole file before the announce day.

## 0. Preconditions (all green before announcing)

- [ ] `main` CI green; hub runs the release commit; 48 h without lock errors / panics.
- [ ] `TestReplayProdHistory` on a fresh prod snapshot passes (515 597+ blocks, minted == hub).
- [ ] `TestV2ForkCrossesActivationEndToEnd` green.
- [ ] Pool: paced budget verified over a full UTC day; `found audit` journal reviewed.
- [ ] Site / API copy ready (v1 until the height, v2 after), RFC final, posts reviewed.
- [ ] Settlement wallet float ≥ 2000 HMC buffer available.

## 1. Pick the height

```bash
bash scripts/ops/v2_height_planner.sh "YYYY-MM-DD 11:00"   # Tue–Thu, 10–14 UTC
```

- Lead ≥ 20 160 blocks (≈ 7 days, RFC D8); the producer guard refuses less.
- Arrival error ≈ ±1 h per 2× hashrate change before the height (ASERT offset = difficulty).
- Avoid 00:xx UTC (pool day reset, checkpoint at 00:10).

## 2. Announce release (one PR)

- `internal/chain/economics_v2.go`: `DefaultEconomicsV2ActivationHeight = <H>`.
- `internal/chain/consensus_v2.go`: `DefaultAuthorityPubKey = "<hub key>"` — the hub signer
  pubkey (`miner_pubkey_ed25519` of any recent hub block). `TestV2CompiledDefaultsConsistent`
  enforces both-or-neither and H > ASERT.
- Version bump, release notes, desktop/apt packages rebuilt (followers need the constants).

## 3. Hub env (announce day, before deploying the release)

```
HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT=<H>
HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM=<H>
HACKME_ECONOMICS_V2_ANNOUNCE_UNIX=<unix of the public announce>   # Dig-only 30 days from here
```

The first block after deploy locks H, the announce time, the authority key and the chain id
into chain meta — **irreversible**. Check the deploy log: `deploy-activate OK`, then
`sqlite3 -readonly /opt/hackme/data/hackme.db "select key,value from meta where key in
('economics_v2_activation_height','economics_v2_announce_unix','authority_pubkey_ed25519','v2_chain_id');"`.

## 4. Start buffer

One public transfer of 2000 HMC to the settlement wallet; put the tx hash in the announce.

## 5. Announce

Height, expected UTC time (±1 h), what changes (0.1 HMC/block, SUP 0.002/block to the pool,
10 % order fee = 5 % burn / 5 % platform, burn 5 → 2.5 → 1.25 %, Dig-only 30 days, chain_id
`hackme-mainnet-1`), what to update (desktop / worker release), the buffer tx hash.

## 6. T−1 day

- [ ] Re-run the planner: still in the window? (If not, the height stays — only the time shifts.)
- [ ] Followers / desktops on the release (status shows the same `tx_chain_id` once v2 is next).
- [ ] Snapshot binaries present (`/opt/hackme/releases`), settler + checkpoint timers green.

## 7. T (first v2 block)

Watch for 30 min after H:

```bash
curl -s https://hackme.tech/pool/api/status | jq '{chain_id, tx_chain_id}'      # hackme-mainnet-1
curl -s https://hackme.tech/pool/api/metrics | jq '{tip_height, econ_base_reward_now_hmc, econ_sup_reward_now}'
curl -s "https://hackme.tech/pool/api/reports/blocks?q=<H>" | jq '.blocks[0]'      # chain_id + reward_to set
curl -s https://hackme.tech/pool/coordinator/api/work/stats | jq '.sup_policy.economics_v2, .pool_budget.daily_budget_hmc'  # true, 288
journalctl -u hackme-node -u hackme-coordinator --since "-30 min" | grep -Ei "v2 header|chain_id|authority|guard|panic"
```

- [ ] Blocks keep coming (watchdog quiet); `reward_to` = settlement wallet; SUP minted per block.
- [ ] HMC settler pays; SUP settler (`workersettle -asset sup`) runs and pays by transfer.
- [ ] A test Dig order with `fee_asset=SUP` and one with HMC open correctly.

## 8. Rollback

- **Before H:** remove the three env lines, redeploy the previous release. If a block was
  produced after the env was set, meta is locked — keep the height (do not edit meta by hand
  without a full DB backup and a written plan).
- **After H:** there is no "back to v1". Fix forward: `releases/<stamp>` binary rollback is for
  crashes only; consensus mistakes need a new announced height.

## 9. After

- Day +1: hourly pool pay, SUP pay, burn counters, checkpoint verify.
- Day +30: Hunt opens automatically (`ANNOUNCE_UNIX` + 30 days) — check one Hunt open.
- v2.1: `state_root` (separate announced height).
