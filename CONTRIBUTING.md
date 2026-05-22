# Contributing

Thanks for your interest in HackMe.

## Before you open a PR

1. Run from the repo root:
   ```bash
   bash scripts/ops/verify_project_health.sh
   ```
2. Keep changes focused; match existing Go and shell style.
3. Do not commit secrets, databases, or local env files (see `.gitignore` and [docs/SECURITY_REPO.md](docs/SECURITY_REPO.md)).
4. User-facing strings in **new** UI or public docs should be **English**.

## Reporting security issues

Do not open public issues for exploitable vulnerabilities. Contact the operator via [hackme.tech/contacts.html](https://hackme.tech/contacts.html) with reproduction steps and impact.

## Releases

Maintainers tag releases after `scripts/release/make_release_bundle.sh` and `public_release_readiness.sh` pass. Binaries are published on the website and GitHub Releases; source is this repository.
