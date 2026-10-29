# Documentation index

## Who are you?

| Role | Start here |
|------|------------|
| **Miner** | [QUICK_START.md](QUICK_START.md) → [SETUP.md](SETUP.md) → [OPEN_POOL_MINERS.md](OPEN_POOL_MINERS.md) → [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md) |
| **Customer** (Dig / Hunt orders) | [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) → [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) → [CUSTOMER_FUZZ_DELIVERABLES.md](CUSTOMER_FUZZ_DELIVERABLES.md) → [ORDER_ECONOMICS.md](ORDER_ECONOMICS.md) |
| **Exchange / listing** | [HMC_TOKENOMICS.md](HMC_TOKENOMICS.md) · [SUP_TOKENOMICS.md](SUP_TOKENOMICS.md) · [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) · [../web/site/token-transparency.html](../web/site/token-transparency.html) · [../web/site/listing.html](../web/site/listing.html) |
| **Economics v2 (upcoming)** | [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md) — emission, pool funding, fees, SUP, activation process |
| **Developer** | [API.md](API.md) → [ARCHITECTURE.md](ARCHITECTURE.md) → [DEVELOPERS_FUZZING.md](DEVELOPERS_FUZZING.md) → [SECURITY.md](SECURITY.md) · [SECURITY_REPO.md](SECURITY_REPO.md) |

Layout: **`docs/`** = public guides · **[`internal/`](internal/README.md)** = operator / listing prep · **[`archive/`](archive/README.md)** = old RC / engine / session notes.

## Start here

| Doc | Audience |
|-----|----------|
| [../README.md](../README.md) | Everyone — overview, ecosystem, quick links |
| [QUICK_START.md](QUICK_START.md) | **1-minute** mine / desktop path |
| [SETUP.md](SETUP.md) | **Miners & desktop** — step-by-step install |
| [SECURITY_REPO.md](SECURITY_REPO.md) | **Contributors** — never commit secrets |
| [ECOSYSTEM_OVERVIEW.md](ECOSYSTEM_OVERVIEW.md) | Multi-coin network map |

## Mining & pool

| Doc | Topic |
|-----|--------|
| [OPEN_POOL_MINERS.md](OPEN_POOL_MINERS.md) | Public pool rules & fairness |
| [NETWORK_MODEL.md](NETWORK_MODEL.md) | Coordinator, settlement, P2P |
| [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md) | CUDA / OpenCL / CPU |
| [BLAKE3_GPU_MATRIX.md](BLAKE3_GPU_MATRIX.md) | Blake3 backend matrix |
| [HACKME_OS.md](HACKME_OS.md) | Bootable rig OS (`MINER_ISO.md` is a stub redirect only) |
| [MINER_WINDOWS_ONE_CLICK.md](MINER_WINDOWS_ONE_CLICK.md) | Windows installer |
| [CUDA_PRODUCTION.md](CUDA_PRODUCTION.md) | CUDA production notes |
| [RIG_PROFILES.md](RIG_PROFILES.md) | GPU rig profiles |
| [MINER_POOL_ECONOMICS.md](MINER_POOL_ECONOMICS.md) | Pool payout economics |
| [ECONOMICS_DASHBOARD.md](ECONOMICS_DASHBOARD.md) | Dashboard metrics |
| [HMS_PUBLIC_ROADMAP.md](HMS_PUBLIC_ROADMAP.md) | HackMe Storage (HMS) — prelaunch |
| [HMS_BACKEND.md](HMS_BACKEND.md) | HMS coordinator + workers |
| [SUPPORT_COIN_UTILITY.md](SUPPORT_COIN_UTILITY.md) | HackMe Support (SUP) accrual |
| [POOL_DIRECT_COORDINATOR.md](POOL_DIRECT_COORDINATOR.md) | Direct coordinator wiring |
| [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md) | Pool-distributed deep campaigns |
| [GHS_DIG_COUPLING.md](GHS_DIG_COUPLING.md) | GHS ↔ Dig coupling |
| Current channel | **0.1.0-rc18.x** — Blake3 · share-PPS · CUDA/OpenCL · ISO — [releases/](releases/) · https://hackme.tech/news.html |
| [../web/site/roadmap.html](../web/site/roadmap.html) | Public ecosystem roadmap |

## Fuzzing / B2B

| Doc / page | Topic |
|------------|--------|
| [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) | Dig packages + Hunt ASAN, packs, coverage, pool anticheat |
| [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) | Hunt 50/50 escrow, packages, pool shards |
| [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) | Coverage Hunt vs libFuzzer |
| [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md) | Coverage engines overview |
| [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md) | Rust inventory + catalog ASAN |
| [HUNT_RUST_PHASE_B.md](HUNT_RUST_PHASE_B.md) | Rust Phase B |
| [CUSTOMER_FUZZ_DELIVERABLES.md](CUSTOMER_FUZZ_DELIVERABLES.md) | Report token, repro, artifacts |
| [DEVELOPERS_FUZZING.md](DEVELOPERS_FUZZING.md) | Orders API, limits, escrow |
| [DIG_HUNT_SKU_TRUST.md](DIG_HUNT_SKU_TRUST.md) | SKU trust / honesty |
| [FUZZ_ESCROW_20_80.md](FUZZ_ESCROW_20_80.md) | Dig escrow settlement split (Hunt is 50/50) |
| [FUZZ_TIER_C.md](FUZZ_TIER_C.md) | Tier C native fuzz |
| [FUZZ_NATIVE_V2.md](FUZZ_NATIVE_V2.md) | Native v2 pipeline |
| [ORDER_ECONOMICS.md](ORDER_ECONOMICS.md) | Escrow, cancellation, pricing |
| [API.md](API.md)#fuzz-campaigns-v1 | Dig `/api/fuzz/*` |
| [API.md](API.md)#hunt-campaigns-phase-2 | Hunt `/api/hunt/*` |
| [../web/site/developers.html](../web/site/developers.html) | Public landing |
| [../web/site/fuzz-guide.html](../web/site/fuzz-guide.html) | Public fuzz guide |
| [../web/site/fuzzing-console.html](../web/site/fuzzing-console.html) | Read-only order tracker |

## Research & disclosures

| Doc / page | Topic |
|------------|--------|
| [BITCOIN30_SERIES.md](BITCOIN30_SERIES.md) | 30-day Bitcoin Core WASM fuzz |
| [OSS_CVE_HUNT.md](OSS_CVE_HUNT.md) | OSS CVE wave pipeline |
| [OSS_CVE_LIBHEIF_SERIES.md](OSS_CVE_LIBHEIF_SERIES.md) | libheif series (closed) |
| [OSS_DISCLOSURE_ACTIONS.md](OSS_DISCLOSURE_ACTIONS.md) | Disclosure status table |
| [OSS_CVE_DISCLOSURE_centijson.md](OSS_CVE_DISCLOSURE_centijson.md) | centijson upstream draft |
| [OSS_CVE_DISCLOSURE_libucl.md](OSS_CVE_DISCLOSURE_libucl.md) | libucl upstream draft |
| [OSS_CVE_DISCLOSURE_cfgpack.md](OSS_CVE_DISCLOSURE_cfgpack.md) | cfgpack upstream draft |
| [BITCOIN_CORE_5MODULE_RESEARCH.md](BITCOIN_CORE_5MODULE_RESEARCH.md) | Bitcoin Core 5-module study |
| [BITCOIN_CORE_OFFICIAL_LINKS.md](BITCOIN_CORE_OFFICIAL_LINKS.md) | Official Bitcoin Core links |
| [L1_CRYPTO_STACK_RESEARCH.md](L1_CRYPTO_STACK_RESEARCH.md) | L1 crypto stack research |
| [L1_CRYPTO_STACK_V3_RESEARCH.md](L1_CRYPTO_STACK_V3_RESEARCH.md) | L1 stack v3 |
| [L1_CRYPTO_STACK_V4_RESEARCH.md](L1_CRYPTO_STACK_V4_RESEARCH.md) | L1 stack v4 |
| [MKPOOL_CASE_STUDY.md](MKPOOL_CASE_STUDY.md) | mkpool case study |
| [TELEGRAM_BOT.md](TELEGRAM_BOT.md) | Operator bot + news channel bot |
| [verdicts/INDEX.md](verdicts/INDEX.md) | Verdict ledger hub |
| [../web/site/research.html](../web/site/research.html) | Public research hub |

## Ecosystem & tokenomics

| Doc / page | Topic |
|------------|--------|
| [HMC_TOKENOMICS.md](HMC_TOKENOMICS.md) | HMC supply, halving, utility |
| [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md) | Public RFC: HMC/SUP/pool v2 + tip+14d activation |
| [SUP_TOKENOMICS.md](SUP_TOKENOMICS.md) | SUP emission & listing policy |
| [SUP_PHASE_C.md](SUP_PHASE_C.md) | SUP Phase C |
| [HMS_TOKENOMICS.md](HMS_TOKENOMICS.md) | HMS token model |
| [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) | Cap table · unlock calendar |
| [RIGHTS_AND_DISCLOSURES.md](RIGHTS_AND_DISCLOSURES.md) | Legal / export disclosures |
| [../web/site/coins.html](../web/site/coins.html) | Ecosystem registry |
| [../web/site/token-transparency.html](../web/site/token-transparency.html) | Live supply APIs + demand disclosure |
| [../web/site/listing.html](../web/site/listing.html) | Listing readiness hub |

## Architecture & security

| Doc | Topic |
|-----|--------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | System design |
| [API.md](API.md) | HTTP API |
| [SECURITY.md](SECURITY.md) | Threat model (local node) |
| [SECURITY_AUDIT_REDTEAM.md](SECURITY_AUDIT_REDTEAM.md) | Red-team notes |
| [BUG_BOUNTY.md](BUG_BOUNTY.md) | Security rewards |
| [TRADEMARK_AND_FORKING.md](TRADEMARK_AND_FORKING.md) | Brand & forks |
| [../spec/CHAIN_SPEC.md](../spec/CHAIN_SPEC.md) | Chain wire format |
| [security/README.md](security/README.md) | Security notes folder |

## Release & developer tooling

| Doc | Topic |
|-----|--------|
| [RELEASE_PACKAGING.md](RELEASE_PACKAGING.md) | Release / ISO packaging |
| [TASK_LANGUAGES.md](TASK_LANGUAGES.md) | WASM task languages |
| [RUST_CPP_TASKS_QUICKSTART.md](RUST_CPP_TASKS_QUICKSTART.md) | Rust/C++ task quickstart |
| [TESTING.md](TESTING.md) | Test matrix |
| [releases/](releases/) | Versioned release notes (incl. rc18) |
| [../scripts/README.md](../scripts/README.md) | Scripts tree layout |
| [../scripts/ops/README.md](../scripts/ops/README.md) | Supported ops scripts |
| [../scripts/release/README.md](../scripts/release/README.md) | Build ISO / Windows / bundles |

## Operator-only

See **[internal/README.md](internal/README.md)** (treasury, VPS capacity, listing memo/pitch, MiningBoard, cutover, monitoring).

Bootstrap customer bots (operator-seeded Dig/Hunt demand): [`../scripts/ops/bootstrap_customer/BOOTSTRAP_README.md`](../scripts/ops/bootstrap_customer/BOOTSTRAP_README.md) — also disclosed on [token-transparency](../web/site/token-transparency.html#bootstrap-demand).

## Archive

Historical RC notes, Hunt engine versions, GPU field logs, and session verdicts: **[archive/README.md](archive/README.md)**.
