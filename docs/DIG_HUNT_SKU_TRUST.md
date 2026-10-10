# Dig/Hunt SKU honesty + CLEAN trust

## Product modes

| Mode | Dig packages | Hunt packages | Seeds | Verify |
|------|--------------|---------------|-------|--------|
| **smoke** | scan, audit | hunt_lite, hunt_standard | merge OFF by default | floor + canary + sampled replay |
| **deep** | deep | hunt_heavy | external/research merge allowed | higher sample %, longer budgets |

Promise language on reports: attested distributed smoke/depth — **not** OSS-Fuzz/CVE replacement.

## Env knobs (hub)

See [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md) for full tables. Rollout: keep `HACKME_POOL_CANARY_PCT=2`, `HACKME_POOL_CLEAN_MIN_MS_PER_EXEC=0`, `HACKME_POOL_CLEAN_EDGES_REQUIRE` off until workers report `edges_touched`. Tighten after soak.

## Worker

Dig submit should include `edges_touched` (unique edges from segment). Legacy omit is allowed until `HACKME_POOL_CLEAN_EDGES_REQUIRE=1`.
