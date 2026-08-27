# HackMe apt repo (L3) — local scaffold only
#
# Goal after D0: `apt update && apt upgrade hackme-node`
# This directory builds an **unsigned local** repo for dry-runs.
# Production needs: GPG-signed Release, HTTPS hosting, stable+rc suites.
#
# Quick local dry-run (no publish):
#   bash scripts/release/linux/build_deb_from_dist.sh
#   bash scripts/release/apt/build_local_apt_repo.sh
#   # then point apt at file://…/dist/apt/repo  (see hackme.list.example)
#
# Production checklist (do not claim done until signed + hosted):
# - [ ] GPG key for HackMe packages (offline / hardware preferred)
# - [ ] suites: stable, rc (optional)
# - [ ] component: main
# - [ ] packages: hackme-node (+ later worker meta-packages)
# - [ ] HTTPS at apt.hackme.tech or hackme.tech/apt/
# - [ ] docs on downloads.html
# - [ ] L1 updater soft-refuses when dpkg owns /opt/hackme/hackme (already)
#
# Until L3 is live, miners use L1: update_hackme_miner.sh / .ps1
