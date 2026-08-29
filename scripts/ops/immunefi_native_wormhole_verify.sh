#!/usr/bin/env bash
# Follow-up: accepted random VAAs — can any pass VerifySignatures with synthetic guardian set?
set -euo pipefail
REPO="${WORMHOLE_REPO:-/tmp/wormhole}"
OUT="${OUT:-/tmp/wormhole-vaa-verify-probe}"
mkdir -p "$OUT"

cat >"$REPO/sdk/vaa/immunefi_verify_probe_test.go" <<'GO'
package vaa_test

import (
	"crypto/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

func TestImmunefiAcceptedVAAVerifyProbe(t *testing.T) {
	addrs := make([]common.Address, 19)
	for i := range addrs {
		addrs[i] = common.BytesToAddress([]byte{byte(i + 1), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(i + 1)})
	}
	var accepted, verified int
	for i := 0; i < 500000; i++ {
		n := 57 + (i % 400)
		buf := make([]byte, n)
		_, _ = rand.Read(buf)
		v, err := vaa.Unmarshal(buf)
		if err != nil || v == nil {
			continue
		}
		accepted++
		if v.VerifySignatures(addrs) {
			verified++
			t.Fatalf("CRITICAL: random VAA verified with synthetic guardians hex=%x", buf)
		}
	}
	t.Logf("accepted=%d verified=%d", accepted, verified)
}
GO

trap 'rm -f "$REPO/sdk/vaa/immunefi_verify_probe_test.go"' EXIT
cd "$REPO/sdk/vaa"
go test -run TestImmunefiAcceptedVAAVerifyProbe -v -count=1 2>&1 | tee "$OUT/verify.log"
grep -E "accepted=|PASS|CRITICAL" "$OUT/verify.log" | tail -5
