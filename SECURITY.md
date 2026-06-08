# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| `0.1.0-rc11m` | Yes (current release candidate) |
| `0.1.0-rc11l`–`rc11i` | Best effort only |
| Older rc / dev builds | Best effort only |

## Reporting a vulnerability

**Please do not** open public GitHub issues for exploitable security bugs.

1. Contact the operators via [https://hackme.tech/contacts.html](https://hackme.tech/contacts.html).
2. Include steps to reproduce, impact, and affected component (node, coordinator, worker, nginx).
3. Allow reasonable time to patch before any public disclosure.

We appreciate responsible disclosure.

**Security rewards (HMC):** discretionary bug bounty for valid private reports — see [docs/BUG_BOUNTY.md](docs/BUG_BOUNTY.md) and https://hackme.tech/security-rewards.html (typical range **1–200 HMC** per issue, ~200 HMC/month cap, not guaranteed).

## Scope

In scope:

- Remote abuse of node, coordinator, or worker HTTP APIs
- Authentication bypass on admin or coordinator routes
- Wallet / settlement logic flaws on the operator stack
- WASM sandbox escape or consensus-breaking PoH behavior

Out of scope (unless chained with the above):

- Social engineering, phishing sites that copy our UI (report to us + hosting provider)
- Miners running outdated forks without checksum verification
- Issues in third-party GPU drivers

## Hardening references

- [docs/SECURITY.md](docs/SECURITY.md) — threat model
- [docs/SECURITY_AUDIT_REDTEAM.md](docs/SECURITY_AUDIT_REDTEAM.md) — pre–open-source checklist
- [docs/BUG_BOUNTY.md](docs/BUG_BOUNTY.md) — reward tiers and reporting rules
- [scripts/ops/public_pool_hardening.env.example](scripts/ops/public_pool_hardening.env.example) — production env template

## Official binaries

Download only from [https://hackme.tech/downloads.html](https://hackme.tech/downloads.html) and verify SHA256 on that page.
