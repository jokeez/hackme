#!/usr/bin/env bash
# Native Go probe: Wormhole VAA Unmarshal — panic / accept on malformed wire bytes.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${OUT:-$ROOT/reports/immunefi/native-wormhole-$(date -u +%Y%m%dT%H%M%SZ)}"
REPO="${WORMHOLE_REPO:-/tmp/wormhole}"
ROUNDS="${ROUNDS:-500000}"

mkdir -p "$OUT"
[[ -d "$REPO/sdk/vaa" ]] || { echo "clone wormhole to $REPO first" >&2; exit 1; }

cat >"$OUT/vaa_unmarshal_probe_test.go" <<'GO'
package vaa_test

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

func TestImmunefiUnmarshalProbe(t *testing.T) {
	rounds := 500000
	if s := os.Getenv("IMMUNEFI_ROUNDS"); s != "" {
		if n, err := parseInt(s); err == nil && n > 0 {
			rounds = n
		}
	}
	var panics, accepted, rejected int
	var sampleAccept []byte
	for i := 0; i < rounds; i++ {
		n := 1 + (i % 512)
		buf := make([]byte, n)
		_, _ = rand.Read(buf)
		func() {
			defer func() {
				if r := recover(); r != nil {
					panics++
				}
			}()
			v, err := vaa.Unmarshal(buf)
			if err != nil {
				rejected++
				return
			}
			accepted++
			if len(sampleAccept) == 0 && v != nil {
				sampleAccept = append([]byte{}, buf...)
			}
		}()
	}
	t.Logf("rounds=%d accepted=%d rejected=%d panics=%d", rounds, accepted, rejected, panics)
	if panics > 0 {
		t.Fatalf("native panic count=%d (potential RCE/DoS candidate)", panics)
	}
	if accepted > 0 {
		t.Logf("sample_accept_hex=%s", hex.EncodeToString(sampleAccept))
	}
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
GO

mv "$OUT/vaa_unmarshal_probe_test.go" "$REPO/sdk/vaa/immunefi_unmarshal_probe_test.go"
trap 'rm -f "$REPO/sdk/vaa/immunefi_unmarshal_probe_test.go"' EXIT

cd "$REPO/sdk/vaa"
IMMUNEFI_ROUNDS="$ROUNDS" go test -run TestImmunefiUnmarshalProbe -v -count=1 2>&1 | tee "$OUT/native.log"
python3 - "$OUT" <<'PY'
import json, pathlib, re, sys
out = pathlib.Path(sys.argv[1])
log = (out / "native.log").read_text()
m = re.search(r"rounds=(\d+) accepted=(\d+) rejected=(\d+) panics=(\d+)", log)
summary = {"probe": "wormhole_vaa_unmarshal", "status": "fail"}
if m:
    summary.update({
        "rounds": int(m.group(1)),
        "accepted": int(m.group(2)),
        "rejected": int(m.group(3)),
        "panics": int(m.group(4)),
    })
if "PASS" in log and summary.get("panics", 0) == 0:
    summary["status"] = "pass"
    summary["bounty_candidate"] = summary.get("accepted", 0) > 0
(out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
PY
