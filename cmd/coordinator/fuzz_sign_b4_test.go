package main

import "testing"

func TestFuzzHybridOffRejectsUnsignedPayoutAddress(t *testing.T) {
	wm := &workManager{hybridSignerEnabled: false}
	ok, reason, _ := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
		WorkerID: "w", MinerAddress: "HMC-1234567890abcdef",
	}, []byte("body"))
	if ok || reason != "hybrid_required_for_payout" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
	ok, _, addr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{WorkerID: "w"}, []byte("body"))
	if !ok || addr != "" {
		t.Fatalf("empty address without hybrid should pass ok=%v addr=%q", ok, addr)
	}
}

func TestFuzzHybridLooseStillRequiresSigForPayoutAddress(t *testing.T) {
	wm := &workManager{hybridSignerEnabled: true, hybridSignerStrict: false}
	ok, reason, _ := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
		WorkerID: "w", MinerAddress: "HMC-1234567890abcdef",
	}, []byte("body"))
	if ok || reason != "signature_required" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}
