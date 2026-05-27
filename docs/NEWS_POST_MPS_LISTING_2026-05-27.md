# HackMe (HMC) listed on MiningPoolStats

HackMe is now visible on MiningPoolStats:  
https://miningpoolstats.app/coins/HMC

## Status (2026-05-28)

VPS incident resolved. Official pool stack is live (`hackme-node`, `coordinator`, `nginx`, settlement timers). Public pool stats API: https://hackme.tech/pool/coordinator/api/pool/stats

Pool registration on MiningPoolStats is submitted; hashrate/workers will appear after MPS indexes the listing.

## Incident note (resolved)

Brief VPS provider cooling/host issue caused intermittent timeout/522. Services restored; `/api/status` optimized for mining load.
