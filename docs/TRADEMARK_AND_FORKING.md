# Trademark, forking, and “stealing” the project

You are publishing **source code**, not giving away your **brand**, **domain**, or **production secrets**. This page explains what others may do legally, what they may not, and what you should do as the original operator.

---

## What AGPL-3.0 means for HackMe

| Others **can** | Others **cannot** (without consequences) |
|----------------|------------------------------------------|
| Read and study the code | Pretend they wrote it first without attribution |
| Fork on GitHub and modify | Remove your `LICENSE` and `NOTICE` |
| Run their own pool with the code | Use the **HackMe** name/logo to impersonate **your** official pool |
| Sell support or hosting for a fork | Register `hackme.tech` look-alike domains to phish miners |
| Contribute back via pull requests | Steal your VPS `.env` files (those were never published) |

**Your moat is operational, not secrecy of the binary:**

- Official domain: **https://hackme.tech**
- Production coordinator + canonical node you operate
- `WORKER_PAYOUT_MAP` and settlement keys on the VPS
- Community trust, checksums, and signed releases
- Trademark on the name **HackMe** / **HackMe Network** (see below)

Forks that compete on **code** are normal in open source. Forks that compete on **your brand** are a legal and community problem.

---

## Protect the name and logo

1. **Do not** put logos under a permissive license in the repo unless you intend others to reuse them. Keep branding in `web/site/assets/` as **project assets**; state in README: *“HackMe name and logo are trademarks of the project operators.”*
2. **Link official downloads only from hackme.tech** — GitHub Releases should mirror the same SHA256 as the website.
3. **Publish first** — timestamp on GitHub + website establishes precedence.
4. Optional: register trademark in your jurisdiction when budget allows.
5. Add `NOTICE` file listing copyright — required by Apache-2.0 anyway.

---

## What to keep private (never in git)

Already in `.gitignore`:

- `.secrets/` — coordinator and admin tokens  
- `.env.desktop`, `.env.vps`, `.env.coord`, `.env.settlement`  
- `data/*.db` — chain state and wallets  
- `logs/` — may contain paths and errors  

**Publishing code does not expose these** unless you commit them by mistake. Run `git status` before every push.

---

## If someone copies the repo

**Legitimate fork:**

- Renames the project (e.g. “AcmePool”)
- Keeps AGPL license and attribution
- Runs on their own domain
- Competes on features — **this is allowed**

**Abuse (act):**

- Same name “HackMe”, same logo, fake `hackme-tech.com` downloads  
- Report: hosting provider, registrar, forum mods, Google Safe Browsing  
- Document your official URLs in README and ANN  

**Security fork (“we fixed your bugs”):**

- Welcome fixes via pull request or responsible disclosure  
- Hostile public exploits without contact — patch quickly, publish advisory in `docs/`

---

## Release hygiene (brand)

1. Push to **your** GitHub org (not a personal throwaway account).  
2. Pin **hackme.tech** as the only official site in README and ANN.  
3. Attach **SHA256SUMS** to GitHub Releases matching the website.  
4. Add [SECURITY.md](SECURITY.md) policy for GitHub.  
5. Enable **branch protection** on `main`.  
6. Keep VPS tokens only on the server (`apply_security_hardening_vps.sh`).

---

## Summary

| Fear | Reality |
|------|---------|
| “They will steal my project” | They can fork **code**, not your **running pool** or **domain** |
| “They will steal miners” | Miners follow **official URLs + checksums** — educate in ANN |
| “They will hack us because source is public” | Security comes from **hardening + tokens + policy**, not hiding code — see [SECURITY_AUDIT_REDTEAM.md](SECURITY_AUDIT_REDTEAM.md) |
| “Someone will sell HackMe” | AGPL allows commercial use of **code**, not your **trademark** or official pool |

Open source is a trade: **transparency and community** in exchange for **attribution and operational leadership**.
