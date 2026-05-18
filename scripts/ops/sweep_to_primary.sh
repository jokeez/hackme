#!/usr/bin/env bash
set -euo pipefail

# Sweep almost all wallet funds to a target address using local node_ed25519.seed.
# Keeps RESERVE_UNITS on source wallet so operational tx can still be sent.
#
# Usage:
#   BASE=http://127.0.0.1:18080 \
#   SEED_FILE=/opt/hackme/data/node_ed25519.seed \
#   TO_ADDRESS=HMC-... \
#   bash scripts/ops/sweep_to_primary.sh
#
# Optional:
#   RESERVE_UNITS=1000000   # keep 0.01 HMC
#   FEE_UNITS=1000
#   MEMO="sweep"
#   WAIT_SEC=90
#   DRY_RUN=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[sweep] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq
require_cmd go

BASE="${BASE:-http://127.0.0.1:8080}"
SEED_FILE="${SEED_FILE:-$ROOT_DIR/data/node_ed25519.seed}"
TO_ADDRESS="${TO_ADDRESS:-}"
RESERVE_UNITS="${RESERVE_UNITS:-1000000}"
FEE_UNITS="${FEE_UNITS:-1000}"
MEMO="${MEMO:-auto-sweep}"
WAIT_SEC="${WAIT_SEC:-90}"
DRY_RUN="${DRY_RUN:-0}"

if [[ -z "$TO_ADDRESS" ]]; then
  echo "[sweep] TO_ADDRESS is required" >&2
  exit 2
fi
if [[ ! -f "$SEED_FILE" ]]; then
  echo "[sweep] seed file not found: $SEED_FILE" >&2
  exit 2
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

wallet_json="$tmp_dir/wallet.json"
curl -fsS "$BASE/api/wallet" >"$wallet_json"

from_addr="$(jq -r '.address // ""' "$wallet_json")"
balance_units="$(jq -r '.balance_units // 0' "$wallet_json")"
nonce="$(jq -r '.next_nonce // 0' "$wallet_json")"

[[ -n "$from_addr" ]] || { echo "[sweep] empty source address" >&2; exit 1; }
[[ "$balance_units" =~ ^[0-9]+$ ]] || { echo "[sweep] bad balance_units: $balance_units" >&2; exit 1; }
[[ "$nonce" =~ ^[0-9]+$ ]] || { echo "[sweep] bad nonce: $nonce" >&2; exit 1; }

if (( balance_units <= RESERVE_UNITS + FEE_UNITS )); then
  echo "[sweep] nothing to sweep: balance_units=$balance_units reserve=$RESERVE_UNITS fee=$FEE_UNITS"
  exit 0
fi

amount_units=$((balance_units - RESERVE_UNITS - FEE_UNITS))
ts_now="$(date +%s)"

unsigned_json="$tmp_dir/unsigned.json"
jq -nc \
  --arg tx_type "transfer_v1" \
  --arg from "$from_addr" \
  --arg to "$TO_ADDRESS" \
  --argjson amount_units "$amount_units" \
  --argjson fee_units "$FEE_UNITS" \
  --argjson nonce "$nonce" \
  --argjson timestamp_unix "$ts_now" \
  --arg memo "$MEMO" \
  '{tx_type:$tx_type,from:$from,to:$to,amount_units:$amount_units,fee_units:$fee_units,nonce:$nonce,timestamp_unix:$timestamp_unix,memo:$memo}' \
  >"$unsigned_json"

signer_go="$tmp_dir/sign_transfer_tmp.go"
cat >"$signer_go" <<'EOF'
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
  TxType string `json:"tx_type"`
  SigAlg string `json:"sig_alg,omitempty"`
  From string `json:"from"`
  To string `json:"to"`
  AmountUnits uint64 `json:"amount_units"`
  FeeUnits uint64 `json:"fee_units"`
  Nonce uint64 `json:"nonce"`
  TimestampUnix int64 `json:"timestamp_unix"`
  Memo string `json:"memo,omitempty"`
  PubKeyEd25519 string `json:"pubkey_ed25519"`
  SigEd25519 string `json:"sig_ed25519"`
}
func canonicalBytes(tx TransferTx) ([]byte, error) {
  wire := struct {
    TxType string `json:"tx_type"`
    SigAlg string `json:"sig_alg,omitempty"`
    From string `json:"from"`
    To string `json:"to"`
    AmountUnits uint64 `json:"amount_units"`
    FeeUnits uint64 `json:"fee_units"`
    Nonce uint64 `json:"nonce"`
    TimestampUnix int64 `json:"timestamp_unix"`
    Memo string `json:"memo,omitempty"`
    PubKeyEd25519 string `json:"pubkey_ed25519"`
  }{
    TxType: tx.TxType,
    SigAlg: strings.TrimSpace(strings.ToLower(tx.SigAlg)),
    From: strings.TrimSpace(tx.From),
    To: strings.TrimSpace(tx.To),
    AmountUnits: tx.AmountUnits,
    FeeUnits: tx.FeeUnits,
    Nonce: tx.Nonce,
    TimestampUnix: tx.TimestampUnix,
    Memo: tx.Memo,
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
    panic("usage: sign_transfer <unsigned.json> <seed_file>")
  }
  raw, err := os.ReadFile(os.Args[1]); if err != nil { panic(err) }
  var tx TransferTx
  if err := json.Unmarshal(raw, &tx); err != nil { panic(err) }
  seedHexRaw, err := os.ReadFile(os.Args[2]); if err != nil { panic(err) }
  seedHex := strings.TrimSpace(string(seedHexRaw))
  seed, err := hex.DecodeString(seedHex)
  if err != nil || len(seed) != ed25519.SeedSize { panic("invalid seed file") }
  priv := ed25519.NewKeyFromSeed(seed)
  pub := priv.Public().(ed25519.PublicKey)
  tx.PubKeyEd25519 = hex.EncodeToString(pub)
  if strings.TrimSpace(tx.From) != addressFromPub(pub) { panic("from != signer address") }
  canon, err := canonicalBytes(tx); if err != nil { panic(err) }
  tx.SigEd25519 = hex.EncodeToString(ed25519.Sign(priv, canon))
  out, err := json.Marshal(tx); if err != nil { panic(err) }
  fmt.Println(string(out))
}
EOF

signed_json="$tmp_dir/signed.json"
go run "$signer_go" "$unsigned_json" "$SEED_FILE" >"$signed_json"

echo "[sweep] from=$from_addr to=$TO_ADDRESS amount_units=$amount_units fee_units=$FEE_UNITS reserve_units=$RESERVE_UNITS nonce=$nonce"
if [[ "$DRY_RUN" == "1" ]]; then
  echo "[sweep] DRY_RUN=1: signed payload at $signed_json"
  exit 0
fi

send_resp="$tmp_dir/send.json"
http_code="$(curl -sS -o "$send_resp" -w '%{http_code}' -X POST "$BASE/api/tx/send" -H "Content-Type: application/json" --data-binary "@$signed_json" || true)"
echo "[sweep] send http=$http_code"
cat "$send_resp"
echo
[[ "$http_code" == "200" ]] || exit 1

tx_hash="$(jq -r '.tx_hash // ""' "$send_resp")"
[[ -n "$tx_hash" ]] || { echo "[sweep] missing tx_hash" >&2; exit 1; }

deadline=$(( $(date +%s) + WAIT_SEC ))
while [[ "$(date +%s)" -le "$deadline" ]]; do
  tx_json="$(curl -fsS "$BASE/api/tx/$tx_hash" || true)"
  st="$(jq -r '.status // ""' <<<"$tx_json" 2>/dev/null || true)"
  if [[ "$st" == "included" || "$st" == "rejected" ]]; then
    echo "[sweep] final_status=$st"
    echo "$tx_json" | jq '.'
    break
  fi
  sleep 1
done

curl -fsS "$BASE/api/wallet" | jq '{address,balance_hmc,balance_units,next_nonce}'
curl -fsS "$BASE/api/address/$TO_ADDRESS" | jq '{address,balance_units,next_nonce}'
