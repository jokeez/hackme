package poolsync

import (
	"testing"

	"hackme/internal/chain"
)

func TestSettleReceiptMACRoundTrip(t *testing.T) {
	rc := SettleApplyReceipt{
		OutboxID: 7, EventID: chain.FuzzSettleEventID("camp-a", 7),
		CampaignID: "camp-a", Kind: "run", Outcome: SettleOutcomeApplied,
		UnitsHash: UnitsHashTerminal(chain.FuzzSettleEventID("camp-a", 7), "camp-a", SettleOutcomeApplied),
	}
	SignSettleReceipt("pub-tok", "customer:a", "secret-a", &rc)
	if rc.MAC == "" {
		t.Fatal("mac empty")
	}
	if !VerifySettleReceiptMAC("pub-tok", "customer:a", "secret-a", rc) {
		t.Fatal("verify failed")
	}
	if VerifySettleReceiptMAC("pub-tok", "customer:a", "wrong", rc) {
		t.Fatal("wrong secret must fail")
	}
	if VerifySettleReceiptMAC("pub-tok", "customer:b", "secret-a", rc) {
		t.Fatal("wrong owner must fail")
	}
}

func TestPrepaidAttestationRoundTrip(t *testing.T) {
	mac := SignPrepaidAttestation("pub", "customer:x", "camp-1", 1.5, 64)
	if !VerifyPrepaidAttestation("pub", "customer:x", "camp-1", 1.5, 64, mac) {
		t.Fatal("verify failed")
	}
	if VerifyPrepaidAttestation("pub", "customer:x", "camp-1", 1.6, 64, mac) {
		t.Fatal("tampered budget must fail")
	}
}

func TestOwnerSecretHashEqual(t *testing.T) {
	h := OwnerSecretHash("abc")
	if !OwnerSecretHashEqual("abc", h) {
		t.Fatal("match")
	}
	if OwnerSecretHashEqual("abd", h) {
		t.Fatal("mismatch")
	}
}
