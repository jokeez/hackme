#!/usr/bin/env bash
# Deploy bootstrap customer bot to remote VPS (from dev machine).
#   BOOTSTRAP_VPS=root@89.150.41.40 BOOTSTRAP_VPS_PASS='...' \
#     bash scripts/ops/bootstrap_customer/deploy_bootstrap_vps.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
HOST="${BOOTSTRAP_VPS:-root@89.150.41.40}"
PASS="${BOOTSTRAP_VPS_PASS:-}"
INSTALL="/opt/hackme-bootstrap"

[[ -n "$PASS" ]] || { echo "set BOOTSTRAP_VPS_PASS" >&2; exit 1; }

python3 <<PY
import os, paramiko, pathlib, sys
root = pathlib.Path("$ROOT")
host = "$HOST".split("@")[-1]
user = "$HOST".split("@")[0] if "@" in "$HOST" else "root"
pw = os.environ.get("BOOTSTRAP_VPS_PASS", "$PASS")
install = "$INSTALL"

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(host, username=user, password=pw, timeout=30)
sftp = c.open_sftp()

def put(local, remote):
    local = pathlib.Path(local)
    sftp.put(str(local), remote)
    print("upload", remote)

def run(cmd, timeout=600):
    print(">>>", cmd[:120])
    stdin, stdout, stderr = c.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode()
    err = stderr.read().decode()
    rc = stdout.channel.recv_exit_status()
    if out: print(out[-4000:])
    if err: print(err[-2000:], file=sys.stderr)
    if rc != 0:
        raise SystemExit(f"rc={rc} cmd={cmd[:80]}")
    return out

run(f"mkdir -p {install}/scripts/bootstrap_customer {install}/tasks/artifacts/security {install}/.secrets")
for name in ["setup_bootstrap_vps.sh", "bootstrap_bot.sh", "place_bootstrap_order.sh", "bootstrap_snapshot.sh", "bootstrap_resync_pool.sh", "workerfuzz_fleet.sh", "workerfuzz_instance.sh"]:
    put(root / "scripts/ops/bootstrap_customer" / name, f"{install}/scripts/bootstrap_customer/{name}")
put(root / "tasks/artifacts/security/rust_script_push_bounds_guard.wasm", f"{install}/tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
# Prefer shipping a fresh workerfuzz binary when present in workspace.
wf = root / "workerfuzz"
if wf.exists():
    run(f"mkdir -p {install}/bin")
    put(wf, f"{install}/bin/workerfuzz")
    run(f"chmod +x {install}/bin/workerfuzz")
# Also ship node binary if workspace has a linux build named hackme (optional).
node_bin = root / "hackme"
if node_bin.exists() and node_bin.stat().st_size > 1_000_000:
    put(node_bin, f"{install}/hackme")
    run(f"chmod +x {install}/hackme")

coord_admin = root / ".secrets/hackme_coordinator_admin_token"
if coord_admin.exists():
    put(coord_admin, f"{install}/.secrets/coordinator_admin.token")
    run(f"chmod 600 {install}/.secrets/coordinator_admin.token")

run(f"chmod +x {install}/scripts/bootstrap_customer/*.sh")
run(f"bash {install}/scripts/bootstrap_customer/setup_bootstrap_vps.sh", timeout=900)

# systemd timer — one order every 36h
run(f"""cat >/etc/systemd/system/hackme-bootstrap-bot.service <<'EOF'
[Unit]
Description=HackMe bootstrap audit order bot
After=hackme-bootstrap-node.service
Wants=hackme-bootstrap-node.service

[Service]
Type=oneshot
Environment=BOOTSTRAP_INSTALL={install}
WorkingDirectory={install}
ExecStart=/bin/bash {install}/scripts/bootstrap_customer/bootstrap_bot.sh
EOF""")

run("""cat >/etc/systemd/system/hackme-bootstrap-bot.timer <<'EOF'
[Unit]
Description=HackMe bootstrap audit bot (every 36h)

[Timer]
OnBootSec=15min
OnUnitActiveSec=36h
Persistent=true

[Install]
WantedBy=timers.target
EOF""")

run("systemctl daemon-reload && systemctl enable hackme-bootstrap-bot.timer && systemctl start hackme-bootstrap-bot.timer")

# First order now (background)
run(f"setsid bash {install}/scripts/bootstrap_customer/bootstrap_bot.sh >> {install}/logs/bootstrap/first_order.nohup.log 2>&1 &", timeout=30)

run(f"systemctl is-active hackme-bootstrap-node.service; systemctl list-timers hackme-bootstrap-bot.timer --no-pager | tail -3")
run(f"curl -fsS http://127.0.0.1:8080/api/wallet -H \"X-Hackme-Admin-Token: $(grep '^HACKME_ADMIN_TOKEN=' {install}/.env | cut -d= -f2-)\" 2>/dev/null | head -c 400 || true")

sftp.close()
c.close()
print("DEPLOY OK")
PY
