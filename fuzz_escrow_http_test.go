package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackme/internal/chain"
)

func TestWriteFuzzEscrowFailedCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
		deny string
	}{
		{chain.ErrFuzzWalletMissing, "escrow_unavailable", "sql:"},
		{chain.ErrFuzzInsufficientBalance, "insufficient_orders_spendable", "sql:"},
		{chain.ErrFuzzEscrowNotFound, "escrow_failed", "sql:"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeFuzzEscrowFailed(rec, tc.err)
		if rec.Code != http.StatusPaymentRequired {
			t.Fatalf("%v: status=%d", tc.err, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != tc.code {
			t.Fatalf("%v: code=%v want %s", tc.err, body["code"], tc.code)
		}
		errStr, _ := body["error"].(string)
		if strings.Contains(errStr, tc.deny) || strings.Contains(errStr, "no rows") {
			t.Fatalf("leaked driver text: %q", errStr)
		}
	}
}
