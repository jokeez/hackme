#!/usr/bin/env python3
"""Deploy/restart worker-vps-msk-01 on Moscow VPS (SSH key or MSK_SSH_PASSWORD)."""
from __future__ import annotations

import os
import pathlib
import pexpect
import sys

HOST = os.environ.get("MSK_HOST", "82.146.53.7")
USER = "root"
PW = os.environ.get("MSK_SSH_PASSWORD", "")
DEPLOY = os.environ.get("MSK_DEPLOY_DIR", "/opt/hackme-worker")
WORKER_ID = os.environ.get("WORKER_ID", "worker-vps-msk-01")
COORD_URL = os.environ.get("COORD_URL", "https://hackme.tech/pool/coordinator")
WALLET = os.environ.get("WALLET", "HMC-91fe007e4036c602")
SECRET_COORD = os.environ["SECRET_COORD"]
SECRET_SEED = os.environ["SECRET_SEED"]


def run(cmd: str, timeout: int = 180) -> int:
    child = pexpect.spawn(cmd, timeout=timeout, encoding="utf-8")
    i = child.expect(["password:", "Password:", pexpect.EOF, pexpect.TIMEOUT])
    if i in (0, 1):
        if not PW:
            print("[msk-repair] SSH needs MSK_SSH_PASSWORD or key auth", file=sys.stderr)
            return 255
        child.sendline(PW)
        child.expect(pexpect.EOF, timeout=timeout)
    out = child.before or ""
    if out.strip():
        print(out[-3000:])
    return child.exitstatus or 0


def main() -> int:
    coord_token = pathlib.Path(SECRET_COORD).read_text().strip()
    seed = pathlib.Path(SECRET_SEED).read_text().strip()

    print(f"[msk-repair] probe {USER}@{HOST}")
    if run(f"ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new {USER}@{HOST} uname -a") != 0:
        if not PW or run(f"ssh -o StrictHostKeyChecking=accept-new {USER}@{HOST} uname -a") != 0:
            print("[msk-repair] SSH failed; install key or set MSK_SSH_PASSWORD", file=sys.stderr)
            return 1

    ssh_opts = "-o StrictHostKeyChecking=accept-new -o BatchMode=yes"

    for c in ["systemctl stop hackme-worker || true"]:
        run(f"ssh {ssh_opts} {USER}@{HOST} {c!r}")

    for local, remote in [("/tmp/workerpoh-msk", f"{DEPLOY}/workerpoh"), ("/tmp/minersign-msk", f"{DEPLOY}/minersign")]:
        if run(f"scp {ssh_opts} {local} {USER}@{HOST}:{remote}") != 0:
            return 1

    env_body = f"""COORD_URL={COORD_URL}
COORD_TOKEN={coord_token}
COORD_ADMIN_TOKEN={coord_token}
WORKER_ID={WORKER_ID}
HACKME_MINER_ED25519_SEED_HEX={seed}
BATCH_SIZE=1048576
HACKME_GPU_DISABLE=1
HACKME_GPU_BACKEND=cpu
HACKME_WORKER_CLAIM_TIMEOUT=90s
HACKME_WORKER_SUBMIT_TIMEOUT=120s
HACKME_WORKER_CLAIM_COOLDOWN_MS=800
HACKME_MINER_NONCE_FILE={DEPLOY}/logs/miner_submit_nonce.{WORKER_ID}.seq
PAYOUT_ADDRESS={WALLET}
"""
    pathlib.Path("/tmp/hackme-msk.env.worker").write_text(env_body)
    if run(f"scp {ssh_opts} /tmp/hackme-msk.env.worker {USER}@{HOST}:{DEPLOY}/.env.worker") != 0:
        return 1

    unit = f"""[Unit]
Description=HackMe pool worker ({WORKER_ID})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory={DEPLOY}
EnvironmentFile={DEPLOY}/.env.worker
ExecStart={DEPLOY}/workerpoh -coord {COORD_URL} -token {coord_token} -worker {WORKER_ID} -batch 1048576 -gpu-disable
Environment=HACKME_WORKER_CLAIM_TIMEOUT=90s
Environment=HACKME_WORKER_SUBMIT_TIMEOUT=120s
Environment=HACKME_WORKER_CLAIM_COOLDOWN_MS=800
Restart=always
RestartSec=5
StandardOutput=append:{DEPLOY}/logs/workerpoh.log
StandardError=append:{DEPLOY}/logs/workerpoh.log

[Install]
WantedBy=multi-user.target
"""
    pathlib.Path("/tmp/hackme-worker.service").write_text(unit)
    if run(f"scp {ssh_opts} /tmp/hackme-worker.service {USER}@{HOST}:/etc/systemd/system/hackme-worker.service") != 0:
        return 1

    for c in [
        f"mkdir -p {DEPLOY}/logs",
        "systemctl stop hackme-worker || true",
        f"chmod 755 {DEPLOY}/workerpoh {DEPLOY}/minersign && chmod 600 {DEPLOY}/.env.worker",
        "systemctl daemon-reload && systemctl enable hackme-worker && systemctl restart hackme-worker",
        "sleep 4; systemctl is-active hackme-worker; tail -15 /opt/hackme-worker/logs/workerpoh.log",
    ]:
        print(f"[msk-repair] >>> {c[:70]}")
        if run(f"ssh {ssh_opts} {USER}@{HOST} {c!r}") != 0:
            return 1

    print("[msk-repair] OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
