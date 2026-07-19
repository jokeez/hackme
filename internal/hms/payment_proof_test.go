package hms

import "testing"

func TestMarketPaymentProofRoundTrip(t *testing.T) {
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "unit-test-secret")
	pay := "hmsp-demo-1"
	qh := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	proof, err := SignMarketPaymentProof(pay, qh, 1.25)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyMarketPaymentProof(pay, qh, proof, 1.25); err != nil {
		t.Fatal(err)
	}
	if err := VerifyMarketPaymentProof(pay, qh, proof, 1.26); err == nil {
		t.Fatal("amount mismatch must fail")
	}
	if err := VerifyMarketPaymentProof("other", qh, proof, 1.25); err == nil {
		t.Fatal("payment_id mismatch must fail")
	}
}

func TestPilotPaymentSkipAllowed(t *testing.T) {
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "")
	if PilotPaymentSkipAllowed() {
		t.Fatal("skip without ALLOW_INSECURE must be false")
	}
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	if !PilotPaymentSkipAllowed() {
		t.Fatal("expected pilot skip allowed")
	}
}
