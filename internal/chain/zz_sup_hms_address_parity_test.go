package chain

import "testing"

// SUP shape must reject every malformed address HMS rejects (fimasw 2026-11-09).
func TestSupHmsTransferShapeAddressParity(t *testing.T) {
	malformed := []string{
		"HMC-dd2d1608462c378",   // 15 hex
		"HMC-a6d09759a4c120190", // 17 hex
		"HMC-a6d09759a4c1201g",  // non-hex
		"HMC-A6D09759A4C12019",  // uppercase
		"HMC-",                  // empty body
		"HMC",                   // no dash body
		"hmc-a6d09759a4c12019",  // wrong prefix case
	}
	for _, a := range malformed {
		if IsValidAddress(a) {
			t.Fatalf("%q unexpectedly valid", a)
		}
		supCode, _ := ValidateSupTransferShape(SupTransferTx{
			TxType: "transfer_sup_v1", From: a, To: "HMC-bbbbbbbbbbbbbbbb",
			AmountUnits: 1, FeeUnits: DefaultSUPTransferMinFee,
		})
		hmsCode, _ := ValidateHmsTransferShape(HmsTransferTx{
			TxType: "transfer_hms_v1", From: a, To: "HMC-bbbbbbbbbbbbbbbb",
			AmountUnits: 1, FeeUnits: DefaultHMSTransferMinFee,
		})
		if hmsCode != "invalid_address" {
			t.Fatalf("HMS %q: want invalid_address got %q", a, hmsCode)
		}
		if supCode != "invalid_address" {
			t.Fatalf("SUP %q: want invalid_address got %q (parity with HMS)", a, supCode)
		}
	}
	// Clean legacy pair still passes both.
	okFrom, okTo := "HMC-aaaaaaaaaaaaaaaa", "HMC-bbbbbbbbbbbbbbbb"
	if code, _ := ValidateSupTransferShape(SupTransferTx{
		TxType: "transfer_sup_v1", From: okFrom, To: okTo,
		AmountUnits: 1, FeeUnits: DefaultSUPTransferMinFee,
	}); code != "" {
		t.Fatalf("SUP clean: %q", code)
	}
	if code, _ := ValidateHmsTransferShape(HmsTransferTx{
		TxType: "transfer_hms_v1", From: okFrom, To: okTo,
		AmountUnits: 1, FeeUnits: DefaultHMSTransferMinFee,
	}); code != "" {
		t.Fatalf("HMS clean: %q", code)
	}
}
