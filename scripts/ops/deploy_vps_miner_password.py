#!/usr/bin/env python3
"""Deploy pool worker on a fresh VPS (password SSH). Do not commit passwords."""
from __future__ import annotations

import os
import pathlib
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]


def main() -> int:
    try:
        import pexpect
    except ImportError:
        print("pip install pexpect (or use .venv-deploy)", file=sys.stderr)
        return 2

    host = os.environ.get("VPS_HOST", "")
    user = os.environ.get("VPS_USER", "root")
    password = os.environ.get("VPS_SSH_PASSWORD", "")
    worker_id = os.environ.get("WORKER_ID", "worker-vps-62-01")
    deploy = os.environ.get("WORKER_DEPLOY_DIR", "/opt/hackme-worker")
    coord_url = os.environ.get("COORD_URL", "https://hackme.tech/pool/coordinator")
    wallet = os.environ.get("WALLET", "").strip()

    secret_worker = ROOT / ".secrets/hackme_coordinator_worker_token"
    secret_coord = ROOT / ".secrets/hackme_coordinator_admin_token"
    secret_seed = ROOT / "data/miner_submit_ed25519_seed.hex"

    if not host or not password:
        print("set VPS_HOST and VPS_SSH_PASSWORD", file=sys.stderr)
        return 2
    if not wallet:
        print("set WALLET=HMC-… (payout address for this worker)", file=sys.stderr)
        return 2

    if secret_worker.is_file():
        coord_token = secret_worker.read_text().strip()
    elif secret_coord.is_file():
        coord_token = secret_coord.read_text().strip()
    else:
        print("missing coordinator token", file=sys.stderr)
        return 2

    if not secret_seed.is_file():
        print("missing miner seed", file=sys.stderr)
        return 2
    seed = secret_seed.read_text().strip()

    worker_bin = ROOT / "workerpoh"
    if (ROOT / "workerpoh-opencl").is_file():
        worker_bin = ROOT / "workerpoh-opencl"
    minersign = ROOT / "minersign"
    if not worker_bin.is_file():
        print(f"build worker first: CGO_ENABLED=0 go build -trimpath -o {worker_bin} ./cmd/workerpoh", file=sys.stderr)
        return 2

    def ssh_cmd(cmd: str, timeout: int = 300) -> tuple[int, str]:
        full = f"ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=20 {user}@{host} {cmd!r}"
        child = pexpect.spawn(full, timeout=timeout, encoding="utf-8")
        i = child.expect(["password:", "Password:", pexpect.EOF, pexpect.TIMEOUT])
        if i in (0, 1):
            child.sendline(password)
            child.expect(pexpect.EOF, timeout=timeout)
        out = (child.before or "") + (child.after if isinstance(child.after, str) else "")
        return child.exitstatus or 0, out

    def scp(local: str, remote: str, timeout: int = 300) -> int:
        full = f"scp -o StrictHostKeyChecking=accept-new {local} {user}@{host}:{remote}"
        child = pexpect.spawn(full, timeout=timeout, encoding="utf-8")
        i = child.expect(["password:", "Password:", pexpect.EOF, pexpect.TIMEOUT])
        if i in (0, 1):
            child.sendline(password)
            child.expect(pexpect.EOF, timeout=timeout)
        return child.exitstatus or 0

    print(f"[deploy] probe {user}@{host}")
    rc, out = ssh_cmd("uname -a && nproc && free -h | head -2 && command -v go || true")
    print(out[-2000:])

    rc, out = ssh_cmd(
        "command -v nvidia-smi >/dev/null && nvidia-smi -L || echo no-nvidia; "
        "command -v clinfo >/dev/null && clinfo 2>/dev/null | grep -E 'Device Name|Device Type' | head -6 || echo no-clinfo"
    )
    print(out[-1500:])

    has_nvidia = "GPU 0:" in out
    gpu_disable = "0" if has_nvidia else "1"
    batch = "4194304" if has_nvidia else "2097152"
    extra_flags = "-gpu-disable" if gpu_disable == "1" else "-gpu-backend auto"

    install = (
        "set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; "
        f"mkdir -p {deploy}/logs; "
        "command -v go >/dev/null || "
        "(apt-get update -qq && apt-get install -y -qq curl ca-certificates golang-go)"
    )
    print("[deploy] install deps")
    rc, out = ssh_cmd(install, timeout=600)
    print(out[-1500:])
    if rc != 0:
        print(f"[deploy] install rc={rc}", file=sys.stderr)
        return 1

    rc, out = ssh_cmd(f"mkdir -p {deploy}/logs && ls -la {deploy}")
    if rc != 0:
        print(out, file=sys.stderr)
        return 1

    print("[deploy] upload binaries")
    if scp(str(worker_bin), f"{deploy}/workerpoh") != 0:
        print("[deploy] scp workerpoh failed", file=sys.stderr)
        return 1
    if minersign.is_file() and scp(str(minersign), f"{deploy}/minersign") != 0:
        print("[deploy] scp minersign failed", file=sys.stderr)
        return 1
    rc, out = ssh_cmd(f"test -x {deploy}/workerpoh && ls -la {deploy}/workerpoh")
    if rc != 0:
        print(out, file=sys.stderr)
        return 1

    nonce_file = f"{deploy}/logs/miner_submit_nonce.{worker_id}.seq"
    nonce_val = int(time.time() * 1000)
    env_body = f"""COORD_URL={coord_url}
COORD_TOKEN={coord_token}
COORD_ADMIN_TOKEN={coord_token}
WORKER_ID={worker_id}
HACKME_MINER_ED25519_SEED_HEX={seed}
BATCH_SIZE={batch}
HACKME_GPU_DISABLE={gpu_disable}
HACKME_WORKER_CLAIM_TIMEOUT=90s
HACKME_WORKER_SUBMIT_TIMEOUT=120s
HACKME_WORKER_CLAIM_COOLDOWN_MS=800
HACKME_MINER_NONCE_FILE={nonce_file}
HACKME_WORKER_HASHRATE_GHS=0.35
PAYOUT_ADDRESS={wallet}
"""
    pathlib.Path("/tmp/hackme-new-vps.env").write_text(env_body)
    scp("/tmp/hackme-new-vps.env", f"{deploy}/.env.worker")

    unit = f"""[Unit]
Description=HackMe pool worker ({worker_id})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory={deploy}
EnvironmentFile={deploy}/.env.worker
ExecStart={deploy}/workerpoh -coord {coord_url} -token {coord_token} -worker {worker_id} -batch {batch} {extra_flags}
Restart=always
RestartSec=5
StandardOutput=append:{deploy}/logs/workerpoh.log
StandardError=append:{deploy}/logs/workerpoh.log

[Install]
WantedBy=multi-user.target
"""
    pathlib.Path("/tmp/hackme-worker-new.service").write_text(unit)
    scp("/tmp/hackme-worker-new.service", "/etc/systemd/system/hackme-worker.service")

    finish = (
        "set -euo pipefail; "
        f"chmod 755 {deploy}/workerpoh; "
        f"chmod 600 {deploy}/.env.worker; "
        f"echo {nonce_val} > {nonce_file}; "
        f"chmod 600 {nonce_file}; "
        "systemctl daemon-reload; "
        "systemctl enable hackme-worker; "
        "systemctl restart hackme-worker; "
        "sleep 5; "
        "systemctl is-active hackme-worker; "
        "pgrep -af workerpoh | head -3; "
        f"tail -25 {deploy}/logs/workerpoh.log"
    )
    print("[deploy] systemd start")
    rc, out = ssh_cmd(finish, timeout=120)
    print(out[-2500:])
    rc2, out2 = ssh_cmd(
        "sleep 6; systemctl is-active hackme-worker; tail -8 " + deploy + "/logs/workerpoh.log",
        timeout=60,
    )
    print(out2[-1200:])
    if "active" not in (out + out2):
        return 1

    print("[deploy] OK", worker_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
