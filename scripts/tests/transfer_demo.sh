#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq
require_cmd go
require_cmd python3

BASE="${BASE:-http://127.0.0.1:8080}"
DATA_DIR="${DATA_DIR:-$ROOT_DIR/data}"
SEED_FILE="${SEED_FILE:-$DATA_DIR/node_ed25519.seed}"
AMOUNT_HMC="${AMOUNT_HMC:-0.1}"
FEE_UNITS="${FEE_UNITS:-1000}"
WAIT_SEC="${WAIT_SEC:-120}"
POLL_SEC="${POLL_SEC:-2}"
RUN_ID="${RUN_ID:-transfer_demo_$(run_id)}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
MINE_IF_STOPPED="${MINE_IF_STOPPED:-0}"

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
OUT="$OUT_DIR/$RUN_ID/transfer_demo"
ensure_reports_dir "$OUT"

if [[ ! -f "$SEED_FILE" ]]; then
  fail "seed file not found: $SEED_FILE"
fi

status_before="$OUT/status_before.json"
wallet_before="$OUT/wallet_before.json"
sender_before="$OUT/sender_before.json"
send_resp="$OUT/send_response.json"
tx_final="$OUT/tx_final.json"
status_after="$OUT/status_after.json"
sender_after="$OUT/sender_after.json"
receiver_after="$OUT/receiver_after.json"
summary="$OUT/summary.json"

curl -x "" -fsS "$BASE/api/status" >"$status_before"
curl -x "" -fsS "$BASE/api/wallet" >"$wallet_before"

sender_addr="$(jq -r '.address // ""' "$wallet_before")"
if [[ -z "$sender_addr" ]]; then
  fail "wallet address is empty"
fi
curl -x "" -fsS "$BASE/api/address/$sender_addr" >"$sender_before"

seed_addr="$(python3 - "$SEED_FILE" <<'PY'
import hashlib
import sys

seed_hex = open(sys.argv[1], "r", encoding="utf-8").read().strip()
seed = bytes.fromhex(seed_hex)
if len(seed) != 32:
    print("")
    raise SystemExit(0)

P = 2**255 - 19
d = -121665 * pow(121666, P - 2, P) % P
I = pow(2, (P - 1) // 4, P)

def xrecover(y):
    xx = (y * y - 1) * pow(d * y * y + 1, P - 2, P)
    x = pow(xx, (P + 3) // 8, P)
    if (x * x - xx) % P != 0:
        x = (x * I) % P
    if x % 2 != 0:
        x = P - x
    return x

By = 4 * pow(5, P - 2, P) % P
Bx = xrecover(By)

def edwards_add(P1, P2):
    x1, y1 = P1
    x2, y2 = P2
    den = pow(1 + d * x1 * x2 * y1 * y2, P - 2, P)
    x3 = (x1 * y2 + x2 * y1) * den % P
    den2 = pow(1 - d * x1 * x2 * y1 * y2, P - 2, P)
    y3 = (y1 * y2 + x1 * x2) * den2 % P
    return (x3, y3)

def scalarmult(P0, e):
    Q = (0, 1)
    while e > 0:
        if e & 1:
            Q = edwards_add(Q, P0)
        P0 = edwards_add(P0, P0)
        e >>= 1
    return Q

h = hashlib.sha512(seed).digest()
a = int.from_bytes(h[:32], "little")
a &= (1 << 254) - 8
a |= (1 << 254)
Ax, Ay = scalarmult((Bx, By), a)
y_bytes = bytearray(Ay.to_bytes(32, "little"))
y_bytes[31] = (y_bytes[31] & 0x7F) | ((Ax & 1) << 7)
pub = bytes(y_bytes)
addr = "HMC-" + hashlib.sha256(pub).hexdigest()[:16]
print(addr)
PY
)"

if [[ -z "$seed_addr" ]]; then
  fail "failed to derive address from seed file"
fi

if [[ "$seed_addr" != "$sender_addr" ]]; then
  jq -nc \
    --arg status "FAIL" \
    --arg reason "wallet_signer_mismatch" \
    --arg sender_wallet "$sender_addr" \
    --arg sender_from_seed "$seed_addr" \
    --arg hint "transfer_v1 requires from == derived(pubkey); current wallet address does not match node_ed25519.seed pubkey" \
    '{status:$status,reason:$reason,sender_wallet:$sender_wallet,sender_from_seed:$sender_from_seed,hint:$hint}' >"$summary"
  fail "wallet/signing mismatch. See $summary"
fi

if [[ "$MINE_IF_STOPPED" == "1" ]]; then
  mining_now="$(jq -r '.mining // false' "$status_before")"
  if [[ "$mining_now" != "true" ]]; then
    if [[ -z "$ADMIN_TOKEN" ]]; then
      fail "MINE_IF_STOPPED=1 requires ADMIN_TOKEN"
    fi
    # Node process must be started with HACKME_CHAIN_LEADER_LOCAL_POH=1 or POST /api/mining/start returns 409.
    curl -x "" -fsS -X POST -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" "$BASE/api/mining/start" >/dev/null
  fi
fi

amount_units="$(python3 - "$AMOUNT_HMC" <<'PY'
import sys
v = float(sys.argv[1])
if v <= 0:
    print(0)
else:
    print(int(v * 100_000_000 + 0.5))
PY
)"
if [[ "$amount_units" == "0" ]]; then
  fail "amount too small after conversion to units"
fi

receiver_addr="HMC-$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"

nonce="$(jq -r '.next_nonce // 0' "$sender_before")"
ts_now="$(date +%s)"
memo="demo:${RUN_ID}"

unsigned_json="$OUT/tx_unsigned.json"
jq -nc \
  --arg tx_type "transfer_v1" \
  --arg from "$sender_addr" \
  --arg to "$receiver_addr" \
  --argjson amount_units "$amount_units" \
  --argjson fee_units "$FEE_UNITS" \
  --argjson nonce "$nonce" \
  --argjson timestamp_unix "$ts_now" \
  --arg memo "$memo" \
  '{
    tx_type:$tx_type, from:$from, to:$to,
    amount_units:$amount_units, fee_units:$fee_units,
    nonce:$nonce, timestamp_unix:$timestamp_unix, memo:$memo
  }' >"$unsigned_json"

tmp_signer="$OUT/sign_transfer_tmp.go"
cat >"$tmp_signer" <<'EOF'
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type TransferTx struct {
	TxType        string `json:"tx_type"`
	From          string `json:"from"`
	To            string `json:"to"`
	AmountUnits   uint64 `json:"amount_units"`
	FeeUnits      uint64 `json:"fee_units"`
	Nonce         uint64 `json:"nonce"`
	TimestampUnix int64  `json:"timestamp_unix"`
	Memo          string `json:"memo,omitempty"`
	PubKeyEd25519 string `json:"pubkey_ed25519"`
	SigEd25519    string `json:"sig_ed25519"`
}

func canonicalBytes(tx TransferTx) ([]byte, error) {
	wire := struct {
		TxType        string `json:"tx_type"`
		From          string `json:"from"`
		To            string `json:"to"`
		AmountUnits   uint64 `json:"amount_units"`
		FeeUnits      uint64 `json:"fee_units"`
		Nonce         uint64 `json:"nonce"`
		TimestampUnix int64  `json:"timestamp_unix"`
		Memo          string `json:"memo,omitempty"`
		PubKeyEd25519 string `json:"pubkey_ed25519"`
	}{
		TxType:        tx.TxType,
		From:          strings.TrimSpace(tx.From),
		To:            strings.TrimSpace(tx.To),
		AmountUnits:   tx.AmountUnits,
		FeeUnits:      tx.FeeUnits,
		Nonce:         tx.Nonce,
		TimestampUnix: tx.TimestampUnix,
		Memo:          tx.Memo,
		PubKeyEd25519: strings.TrimSpace(tx.PubKeyEd25519),
	}
	return json.Marshal(wire)
}

func addressFromPub(pub []byte) string {
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: sign_transfer <unsigned.json> <seed_file>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var tx TransferTx
	if err := json.Unmarshal(raw, &tx); err != nil {
		panic(err)
	}

	seedHexRaw, err := os.ReadFile(os.Args[2])
	if err != nil {
		panic(err)
	}
	seedHex := strings.TrimSpace(string(seedHexRaw))
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		panic("invalid seed file")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	tx.PubKeyEd25519 = hex.EncodeToString(pub)
	canon, err := canonicalBytes(tx)
	if err != nil {
		panic(err)
	}
	tx.SigEd25519 = hex.EncodeToString(ed25519.Sign(priv, canon))

	out, err := json.Marshal(tx)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
}
EOF

signed_json="$OUT/tx_signed.json"
go run "$tmp_signer" "$unsigned_json" "$SEED_FILE" >"$signed_json"
rm -f "$tmp_signer"

send_http="$(curl -x "" -sS -o "$send_resp" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" --data-binary "@$signed_json" || true)"
if [[ "$send_http" != "200" ]]; then
  fail "send failed http=$send_http body=$(cat "$send_resp")"
fi
tx_hash="$(jq -r '.tx_hash // ""' "$send_resp")"
if [[ -z "$tx_hash" ]]; then
  fail "missing tx_hash in send response"
fi

deadline=$(( $(date +%s) + WAIT_SEC ))
final_status=""
while [[ "$(date +%s)" -le "$deadline" ]]; do
  if curl -x "" -fsS "$BASE/api/tx/$tx_hash" >"$tx_final"; then
    final_status="$(jq -r '.status // ""' "$tx_final")"
    if [[ "$final_status" == "included" || "$final_status" == "rejected" ]]; then
      break
    fi
  fi
  sleep "$POLL_SEC"
done

if [[ -z "$final_status" ]]; then
  final_status="timeout"
fi

curl -x "" -fsS "$BASE/api/status" >"$status_after"
curl -x "" -fsS "$BASE/api/address/$sender_addr" >"$sender_after"
curl -x "" -fsS "$BASE/api/address/$receiver_addr" >"$receiver_after"

python3 - "$sender_before" "$sender_after" "$receiver_after" "$status_before" "$status_after" "$send_resp" "$tx_final" "$summary" "$amount_units" "$FEE_UNITS" "$sender_addr" "$receiver_addr" "$final_status" <<'PY'
import json
import sys

sender_before_p, sender_after_p, receiver_after_p = sys.argv[1], sys.argv[2], sys.argv[3]
status_before_p, status_after_p = sys.argv[4], sys.argv[5]
send_resp_p, tx_final_p, summary_p = sys.argv[6], sys.argv[7], sys.argv[8]
amount_units = int(sys.argv[9])
fee_units = int(sys.argv[10])
sender_addr = sys.argv[11]
receiver_addr = sys.argv[12]
final_status = sys.argv[13]

def readj(p):
    with open(p, "r", encoding="utf-8") as f:
        return json.load(f)

sb = readj(sender_before_p)
sa = readj(sender_after_p)
ra = readj(receiver_after_p)
stb = readj(status_before_p)
sta = readj(status_after_p)
sr = readj(send_resp_p)
tf = readj(tx_final_p) if final_status != "timeout" else {}

burn_before = float((stb.get("economics") or {}).get("total_burned_hmc") or 0.0)
burn_after = float((sta.get("economics") or {}).get("total_burned_hmc") or 0.0)
burn_delta = burn_after - burn_before

sender_before_units = int(sb.get("balance_units") or 0)
sender_after_units = int(sa.get("balance_units") or 0)
receiver_after_units = int(ra.get("balance_units") or 0)

expected_sender_delta = -(amount_units + fee_units)
actual_sender_delta = sender_after_units - sender_before_units
expected_receiver_delta = amount_units
actual_receiver_delta = receiver_after_units
expected_burn_units = int(float(fee_units) * 0.3)
expected_burn_hmc = expected_burn_units / 100_000_000.0

checks = {
    "tx_sent_ok": bool(sr.get("ok") is True),
    "tx_included": final_status == "included",
    "sender_delta_match": actual_sender_delta == expected_sender_delta if final_status == "included" else False,
    "receiver_delta_match": actual_receiver_delta == expected_receiver_delta if final_status == "included" else False,
    "burn_delta_nonzero": burn_delta > 0 if final_status == "included" else False,
}

summary = {
    "status": "PASS" if all(checks.values()) else "FAIL",
    "final_tx_status": final_status,
    "tx_hash": sr.get("tx_hash"),
    "sender": sender_addr,
    "receiver": receiver_addr,
    "amount_units": amount_units,
    "fee_units": fee_units,
    "expected": {
        "sender_delta_units": expected_sender_delta,
        "receiver_delta_units": expected_receiver_delta,
        "burn_delta_hmc_approx": expected_burn_hmc,
    },
    "actual": {
        "sender_before_units": sender_before_units,
        "sender_after_units": sender_after_units,
        "sender_delta_units": actual_sender_delta,
        "receiver_after_units": receiver_after_units,
        "receiver_delta_units": actual_receiver_delta,
        "burn_before_hmc": burn_before,
        "burn_after_hmc": burn_after,
        "burn_delta_hmc": burn_delta,
        "tx_row": tf,
    },
    "checks": checks,
}

with open(summary_p, "w", encoding="utf-8") as f:
    json.dump(summary, f, ensure_ascii=False, indent=2)

print(json.dumps(summary, ensure_ascii=False))
if summary["status"] != "PASS":
    sys.exit(1)
PY

pass "transfer demo PASS. Report: $summary"
