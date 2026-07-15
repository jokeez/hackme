# Quick start (~1 minute)

Mine on the live pool or run a desktop node. Full detail: [SETUP.md](SETUP.md).

## Windows / HackMe OS (fastest)

1. Open [downloads](https://hackme.tech/downloads.html) → install **Win** or flash **HackMe OS** ISO  
2. Verify **SHA256** on that page  
3. Start **HackMe Miner** (or boot USB) → confirm wallet · workers appear on the pool  

## Linux (from source)

```bash
git clone https://github.com/jokeez/hackme.git
cd hackme
cp .env.desktop.example .env.desktop
bash scripts/ops/start_pool_miner.sh
```

Dashboard: **http://127.0.0.1:8080** · Official coordinator: `https://hackme.tech/pool/coordinator`

## Check the pool (no secrets)

```bash
curl -fsS https://hackme.tech/pool/coordinator/api/work/stats \
  | jq '{workers_online,pool_hashrate_gh_s,target_mod,total_payout_hmc}'
```

## Next

| Goal | Doc |
|------|-----|
| Full install & GPU | [SETUP.md](SETUP.md) |
| Operator “is prod OK?” | [STATUS.md](STATUS.md) |
| B2B fuzz | [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) |
| Docs map | [INDEX.md](INDEX.md) |
