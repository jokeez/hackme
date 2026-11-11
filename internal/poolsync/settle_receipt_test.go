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

func TestHubPrepaidTicketRoundTripAndSelfMACRefused(t *testing.T) {
	fp := EscrowPrepaidFingerprintParts("camp-1", 1.5, 64, "open")
	exp := int64(1_900_000_000)
	ticket := SignHubPrepaidTicket("hub-secret", "customer:x", "camp-1", 1.5, 64, fp, exp)
	if !IsHubPrepaidTicket(ticket) {
		t.Fatalf("want hubv1 prefix, got %q", ticket)
	}
	if !VerifyHubPrepaidTicket("hub-secret", "customer:x", "camp-1", 1.5, 64, fp, ticket, exp-10) {
		t.Fatal("hub verify failed")
	}
	if VerifyHubPrepaidTicket("hub-secret", "customer:x", "camp-1", 1.5, 64, fp, ticket, exp+1) {
		t.Fatal("expired ticket must fail")
	}
	if VerifyHubPrepaidTicket("wrong", "customer:x", "camp-1", 1.5, 64, fp, ticket, exp-10) {
		t.Fatal("wrong hub secret must fail")
	}
	self := SignPrepaidAttestation("pub", "customer:x", "camp-1", 1.5, 64)
	if VerifyPublishPrepaidAttestation("pub", "hub-secret", "customer:x", "camp-1", 1.5, 64, fp, self, exp-10, true) {
		t.Fatal("requireHub must refuse forgeable self-MAC")
	}
	if !VerifyPublishPrepaidAttestation("pub", "hub-secret", "customer:x", "camp-1", 1.5, 64, fp, ticket, exp-10, true) {
		t.Fatal("requireHub must accept hub ticket")
	}
	if VerifyPrepaidAttestation("pub", "customer:x", "camp-1", 1.5, 64, ticket) {
		t.Fatal("self-MAC verifier must not accept hub tickets")
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
