#!/usr/bin/env bash
# Print VPS inventory table for ops / audit pack (Markdown + JSON).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${VPS_INVENTORY_OUT:-$ROOT/reports/vps-inventory-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$OUT"
NODE_SSH="${NODE_SSH:-hackme-vps}"

hub_line="$(ssh -o BatchMode=yes -o ConnectTimeout=12 "$NODE_SSH" '
  ipj=$(curl -fsS --max-time 6 ipinfo.io/json 2>/dev/null || echo "{}")
  commit=$(curl -fsS --max-time 6 http://127.0.0.1:18080/api/status 2>/dev/null | jq -r ".commit // empty")
  load=$(cut -d" " -f1-3 /proc/loadavg)
  disk=$(df -h / | tail -1 | awk "{print \$3\"/\"\$2\" (\"\$5\")\"}")
  echo "$ipj" | jq -c --arg load "$load" --arg disk "$disk" --arg commit "$commit" \
    "{role:\"hmc_hub_primary\",ssh:\"hackme-vps\",ip:.ip,asn_org:.org,country:.country,city:.city,load:\$load,disk_root:\$disk,deploy_commit:\$commit,status:\"live\",services:\"node,coordinator,nginx,settlement\"}"
' 2>/dev/null || echo '{"role":"hmc_hub_primary","status":"unreachable"}')"

python3 - "$OUT" "$hub_line" <<'PY'
import json, sys, os
out, hub_raw = sys.argv[1], sys.argv[2]
hub = json.loads(hub_raw)
data = {
    "generated_utc": __import__("datetime").datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "hosts": [
        hub,
        {"role": "b2b_customer_node", "ip": "89.150.41.40", "status": "live", "note": "fuzz escrow + bootstrap PoH"},
        {"role": "hmc_mirror_standby", "ip": None, "status": "planned", "target_provider": "Hetzner CPX21 DE/FI AS24940"},
        {"role": "exchange_api", "ip": None, "status": "deferred", "note": "after mirror drill"},
        {"role": "hms_heavy", "ip": None, "status": "reserved", "note": "prelaunch"},
    ],
}
json.dump(data, open(f"{out}/inventory.json", "w"), indent=2)
md = ["# VPS inventory snapshot", "", f"**Generated:** {data['generated_utc']}", "",
      "| Role | IP | Provider | Status | Notes |", "|------|-----|----------|--------|-------|"]
for h in data["hosts"]:
    ip = h.get("ip") or "—"
    prov = h.get("asn_org") or h.get("target_provider") or "—"
    st = h.get("status", "live")
    note = h.get("services") or h.get("note") or ""
    c = h.get("deploy_commit")
    if c:
        note = f"{c[:12]}; {note}"
    md.append(f"| {h['role']} | {ip} | {prov} | {st} | {note} |")
open(f"{out}/INVENTORY.md", "w").write("\n".join(md) + "\n")
print(f"[vps-inventory] wrote {out}/INVENTORY.md")
print(open(f"{out}/INVENTORY.md").read())
PY
