# Canonical release checks (schema + fuzz on prod-base)

## Why local follower showed `schema_version: 0`

Follower/worker nodes intentionally skipped reading local SQLite for parts of `/api/status` for latency. **`schema_version`** must always reflect **`PRAGMA user_version`** on the node disk so gates see migration drift.

Current builds also **`mergeCanonicalEconomicsIntoStatus`**: when `HACKME_CANONICAL_CHAIN_URL` (or inferred canon base) points at a **remote** host, `/api/status` overlays **`economics`**, **`crypto_policy`**, and **`consensus_policy`** from the canon `/api/status` even while tip sync follows canon — so **`fuzz_release_gate`** economics checks work on followers pointing at hackme.tech.

## Commands

### Private infra gate (schema, hardware snapshot, optional fuzz auth probes)

```bash
ADMIN_TOKEN='<admin-on-that-host>' \
BASE='https://hackme.tech' \
COORD='https://hackme.tech/pool/coordinator' \
bash scripts/ops/private_stage_gate.sh
```

Expect **`schema-version-match`** = pass when local DB migrated to **`schema_expected`** (see `store.CurrentSchemaVersion` in code).

### Full fuzz/language release gate (same script as CI)

```bash
ADMIN_TOKEN='<admin-on-that-host>' \
BASE='https://hackme.tech' \
bash scripts/ops/fuzz_release_gate.sh
```

**Warning:** This creates campaigns and posts artifacts on the **target** node — run only against staging or when operators accept test data on prod.

### Local follower sanity

After upgrading the binary, hit `/api/status` on a follower with canon URL set: **`schema_version`** should equal **`schema_expected`**, and **`economics`** should be non-null when canon is reachable.
