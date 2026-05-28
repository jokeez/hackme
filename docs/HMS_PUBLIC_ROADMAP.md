# HMS (HackMe Storage) — public roadmap

**Status:** Prelaunch · UI preview on site and local dashboard · **not enabled on hackme.tech hub VPS**

HMS is the storage lane of the useful pool: encrypted backups, disk Proof-of-Storage, and hourly **manifest seal** (SHA256, ASIC-friendly). Full technical spec lives in local adapter docs (`adapters/hms/`, not deployed).

## For miners

| Role | Hardware | Reward (planned) |
|------|----------|------------------|
| Storage | HDD/SSD quota | HMS for proved GB×hour |
| Seal | ASIC or CPU | HMS for epoch seal blocks |

Disk and seal work together at the **network** level (no seal ⇒ no payout epoch). Combo rig on one PC gets a bonus (TBD).

## For clients

B2B encrypted backup (client-side keys). Pay in HMC/SUP at upload; chunks never stored in plaintext on hub or miner disks.

## Infrastructure

- **Hub VPS (now):** HMC, SUP, website — unchanged  
- **Heavy VPS #2 (before go-live):** HMS coordinator, ingress, seal Stratum  
- **MPS:** add **HMS** under existing **HackMe Official Pool** (same brand, new coin row)

## Timeline

1. Design + local mocks (current)  
2. Buy Amsterdam heavy VPS ~1 week before launch  
3. Pilot 10–50 GB · restore gate  
4. Public lane + settlement  

Questions: support@hackme.tech
