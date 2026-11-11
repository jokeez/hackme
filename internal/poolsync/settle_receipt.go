package poolsync

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"hackme/internal/chain"
)

// SettleApplyReceipt is an origin-verified proof that ApplyFuzzSettleOnce (or a
// terminal drain outcome) ran for one outbox row. Bare confirm_applied bools are
// not accepted on the coordinator ACK path.
type SettleApplyReceipt struct {
	OutboxID   int64  `json:"outbox_id"`
	EventID    string `json:"event_id"`
	CampaignID string `json:"campaign_id"`
	Kind       string `json:"kind"`
	Outcome    string `json:"outcome"` // applied|depleted|already_paid|drain_close
	UnitsHash  string `json:"units_hash"`
	MAC        string `json:"mac"`
}

// SettleReceiptOutcomes accepted on ACK.
const (
	SettleOutcomeApplied     = "applied"
	SettleOutcomeDepleted    = "depleted"
	SettleOutcomeAlreadyPaid = "already_paid"
	SettleOutcomeDrainClose  = "drain_close"
)

// SettleReceiptMACKey derives the HMAC key for settle receipts.
// publishToken + ownerRef + ownerSecret bind the receipt to a non-spoofable tenant
// capability (shared publish token alone is not enough).
func SettleReceiptMACKey(publishToken, ownerRef, ownerSecret string) []byte {
	h := sha256.New()
	h.Write([]byte("hackme-settle-ack-v1\n"))
	h.Write([]byte(strings.TrimSpace(publishToken)))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.TrimSpace(ownerRef)))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.TrimSpace(ownerSecret)))
	return h.Sum(nil)
}

// CanonicalSettleReceiptMessage is the HMAC message for a receipt.
func CanonicalSettleReceiptMessage(r SettleApplyReceipt) string {
	return strings.Join([]string{
		strconv.FormatInt(r.OutboxID, 10),
		strings.TrimSpace(r.EventID),
		strings.TrimSpace(r.CampaignID),
		strings.ToLower(strings.TrimSpace(r.Kind)),
		strings.ToLower(strings.TrimSpace(r.Outcome)),
		strings.ToLower(strings.TrimSpace(r.UnitsHash)),
	}, "|")
}

// SignSettleReceipt fills MAC using the tenant settle key.
func SignSettleReceipt(publishToken, ownerRef, ownerSecret string, r *SettleApplyReceipt) {
	if r == nil {
		return
	}
	mac := hmac.New(sha256.New, SettleReceiptMACKey(publishToken, ownerRef, ownerSecret))
	_, _ = mac.Write([]byte(CanonicalSettleReceiptMessage(*r)))
	r.MAC = hex.EncodeToString(mac.Sum(nil))
}

// VerifySettleReceiptMAC checks a receipt MAC (constant-time).
func VerifySettleReceiptMAC(publishToken, ownerRef, ownerSecret string, r SettleApplyReceipt) bool {
	if strings.TrimSpace(r.MAC) == "" || strings.TrimSpace(ownerSecret) == "" {
		return false
	}
	mac := hmac.New(sha256.New, SettleReceiptMACKey(publishToken, ownerRef, ownerSecret))
	_, _ = mac.Write([]byte(CanonicalSettleReceiptMessage(r)))
	want, err := hex.DecodeString(strings.TrimSpace(r.MAC))
	if err != nil {
		return false
	}
	return hmac.Equal(want, mac.Sum(nil))
}

// UnitsHashFromEscrow fingerprints post-Apply escrow units (event_id+campaign+units).
func UnitsHashFromEscrow(eventID, campaignID string, row *chain.FuzzEscrowRow) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(eventID))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(campaignID))
	if row != nil {
		fmt.Fprintf(&b, "|%.8f|%.8f|%.8f|%.8f|%.8f|%d|%s",
			row.BudgetHMC, row.RunsPaidHMC, row.BountyPaidHMC, row.CrashBonusPaidHMC,
			row.RefundedBountyHMC, row.RunsDone, strings.TrimSpace(row.Status))
	} else {
		b.WriteString("|nil")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// UnitsHashTerminal is used when Apply did not return a row (drain / already-paid).
func UnitsHashTerminal(eventID, campaignID, outcome string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(eventID),
		strings.TrimSpace(campaignID),
		strings.ToLower(strings.TrimSpace(outcome)),
		"terminal",
	}, "|")))
	return hex.EncodeToString(sum[:])
}

// PrepaidAttestationMACKey binds prepaid claims under the shared publish token + owner.
func PrepaidAttestationMACKey(publishToken, ownerRef string) []byte {
	h := sha256.New()
	h.Write([]byte("hackme-prepaid-attest-v1\n"))
	h.Write([]byte(strings.TrimSpace(publishToken)))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.TrimSpace(ownerRef)))
	return h.Sum(nil)
}

// CanonicalPrepaidAttestation message for HMAC.
func CanonicalPrepaidAttestation(campaignID string, budgetHMC float64, budgetRuns int) string {
	return fmt.Sprintf("%s|%.4f|%d", strings.TrimSpace(campaignID), budgetHMC, budgetRuns)
}

// SignPrepaidAttestation returns hex HMAC for a prepaid claim.
func SignPrepaidAttestation(publishToken, ownerRef, campaignID string, budgetHMC float64, budgetRuns int) string {
	mac := hmac.New(sha256.New, PrepaidAttestationMACKey(publishToken, ownerRef))
	_, _ = mac.Write([]byte(CanonicalPrepaidAttestation(campaignID, budgetHMC, budgetRuns)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPrepaidAttestation checks a prepaid claim MAC.
func VerifyPrepaidAttestation(publishToken, ownerRef, campaignID string, budgetHMC float64, budgetRuns int, got string) bool {
	want, err := hex.DecodeString(strings.TrimSpace(got))
	if err != nil || len(want) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, PrepaidAttestationMACKey(publishToken, ownerRef))
	_, _ = mac.Write([]byte(CanonicalPrepaidAttestation(campaignID, budgetHMC, budgetRuns)))
	return hmac.Equal(want, mac.Sum(nil))
}

// OwnerSecretHash stores a non-reversible form of the per-publish owner secret.
func OwnerSecretHash(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

// OwnerSecretHashEqual compares a presented secret to a stored hash.
func OwnerSecretHashEqual(secret, storedHash string) bool {
	storedHash = strings.ToLower(strings.TrimSpace(storedHash))
	if storedHash == "" || strings.TrimSpace(secret) == "" {
		return false
	}
	got := OwnerSecretHash(secret)
	if len(got) != len(storedHash) {
		return false
	}
	return hmac.Equal([]byte(got), []byte(storedHash))
}
