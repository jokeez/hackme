package poolsync

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// UnitsHashFromOutbox is the hub-verifiable ACK units fingerprint (PUB-07).
// Bound only to outbox fields the hub already stores + claimed outcome — never to
// tenant-supplied escrow balances (those were honor-system and let false ACKs invent units).
func UnitsHashFromOutbox(eventID, campaignID, kind, minerAddress, severity string, workItemID int64, outcome string) string {
	if workItemID < 0 {
		workItemID = 0
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"hackme-settle-units-v2",
		strings.TrimSpace(eventID),
		strings.TrimSpace(campaignID),
		strings.ToLower(strings.TrimSpace(kind)),
		strings.TrimSpace(minerAddress),
		strings.ToLower(strings.TrimSpace(severity)),
		strconv.FormatInt(workItemID, 10),
		strings.ToLower(strings.TrimSpace(outcome)),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

// UnitsHashFromEscrow fingerprints post-Apply escrow units (event_id+campaign+units).
// Deprecated for settle ACK: hub cannot verify tenant escrow snapshots (PUB-07).
// Prefer UnitsHashFromOutbox. Kept for diagnostics / older lab tooling.
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

// UnitsHashTerminal is a legacy drain fingerprint (event+campaign+outcome only).
// Prefer UnitsHashFromOutbox for ACK; hub verify rejects this when outbox binding is enforced.
func UnitsHashTerminal(eventID, campaignID, outcome string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(eventID),
		strings.TrimSpace(campaignID),
		strings.ToLower(strings.TrimSpace(outcome)),
		"terminal",
	}, "|")))
	return hex.EncodeToString(sum[:])
}

// PrepaidAttestationMACKey binds lab/self prepaid claims under the shared publish
// token + owner. Forgeable by any publish-token holder — refused when
// RequireHubPrepaidTicket() (minting-hub / D1). Prefer SignHubPrepaidTicket.
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

// SignPrepaidAttestation returns hex HMAC for a lab prepaid claim (publish-token key).
// Do not use under HACKME_CHAIN_MINTING_HUB=1 — coordinator refuses self-MAC there.
func SignPrepaidAttestation(publishToken, ownerRef, campaignID string, budgetHMC float64, budgetRuns int) string {
	mac := hmac.New(sha256.New, PrepaidAttestationMACKey(publishToken, ownerRef))
	_, _ = mac.Write([]byte(CanonicalPrepaidAttestation(campaignID, budgetHMC, budgetRuns)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPrepaidAttestation checks a lab/self prepaid claim MAC.
func VerifyPrepaidAttestation(publishToken, ownerRef, campaignID string, budgetHMC float64, budgetRuns int, got string) bool {
	got = strings.TrimSpace(got)
	if IsHubPrepaidTicket(got) {
		return false
	}
	want, err := hex.DecodeString(got)
	if err != nil || len(want) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, PrepaidAttestationMACKey(publishToken, ownerRef))
	_, _ = mac.Write([]byte(CanonicalPrepaidAttestation(campaignID, budgetHMC, budgetRuns)))
	return hmac.Equal(want, mac.Sum(nil))
}

// Hub prepaid tickets (minting-hub / D1). Secret ≠ publish token so a leaked
// fleet publish credential alone cannot enqueue unpaid pool work.

const (
	HubPrepaidTicketPrefix     = "hubv1:"
	DefaultHubPrepaidTicketTTL = 2 * 3600 // seconds
)

// EscrowPrepaidFingerprint commits to an open escrow row (campaign + budget + runs + status).
func EscrowPrepaidFingerprint(row *chain.FuzzEscrowRow) string {
	if row == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%.8f|%d|%s",
		strings.TrimSpace(row.CampaignID), row.BudgetHMC, row.BudgetRuns,
		strings.ToLower(strings.TrimSpace(row.Status)))))
	return hex.EncodeToString(sum[:])
}

// EscrowPrepaidFingerprintParts builds the same fingerprint without a row object.
func EscrowPrepaidFingerprintParts(campaignID string, budgetHMC float64, budgetRuns int, status string) string {
	return EscrowPrepaidFingerprint(&chain.FuzzEscrowRow{
		CampaignID: campaignID, BudgetHMC: budgetHMC, BudgetRuns: budgetRuns, Status: status,
	})
}

// HubPrepaidTicketMACKey derives the HMAC key for hub-signed prepaid tickets.
func HubPrepaidTicketMACKey(hubSecret string) []byte {
	h := sha256.New()
	h.Write([]byte("hackme-prepaid-hub-ticket-v1\n"))
	h.Write([]byte(strings.TrimSpace(hubSecret)))
	return h.Sum(nil)
}

// CanonicalHubPrepaidTicket is the HMAC message for a hub ticket.
func CanonicalHubPrepaidTicket(ownerRef, campaignID string, budgetHMC float64, budgetRuns int, escrowFP string, expUnix int64) string {
	return strings.Join([]string{
		strings.TrimSpace(ownerRef),
		strings.TrimSpace(campaignID),
		fmt.Sprintf("%.4f", budgetHMC),
		strconv.Itoa(budgetRuns),
		strings.ToLower(strings.TrimSpace(escrowFP)),
		strconv.FormatInt(expUnix, 10),
	}, "|")
}

// IsHubPrepaidTicket reports whether the attestation uses the hubv1 wire format.
func IsHubPrepaidTicket(got string) bool {
	return strings.HasPrefix(strings.TrimSpace(got), HubPrepaidTicketPrefix)
}

// SignHubPrepaidTicket returns hubv1:<exp>:<mac> bound to escrow fingerprint.
func SignHubPrepaidTicket(hubSecret, ownerRef, campaignID string, budgetHMC float64, budgetRuns int, escrowFP string, expUnix int64) string {
	hubSecret = strings.TrimSpace(hubSecret)
	escrowFP = strings.ToLower(strings.TrimSpace(escrowFP))
	if hubSecret == "" || escrowFP == "" || expUnix <= 0 {
		return ""
	}
	mac := hmac.New(sha256.New, HubPrepaidTicketMACKey(hubSecret))
	_, _ = mac.Write([]byte(CanonicalHubPrepaidTicket(ownerRef, campaignID, budgetHMC, budgetRuns, escrowFP, expUnix)))
	return HubPrepaidTicketPrefix + strconv.FormatInt(expUnix, 10) + ":" + hex.EncodeToString(mac.Sum(nil))
}

// ParseHubPrepaidTicket splits hubv1:<exp>:<mac>.
func ParseHubPrepaidTicket(got string) (expUnix int64, macHex string, ok bool) {
	got = strings.TrimSpace(got)
	if !strings.HasPrefix(got, HubPrepaidTicketPrefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(got, HubPrepaidTicketPrefix)
	expStr, macHex, cut := strings.Cut(rest, ":")
	if !cut || macHex == "" {
		return 0, "", false
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || expUnix <= 0 {
		return 0, "", false
	}
	return expUnix, macHex, true
}

// VerifyHubPrepaidTicket checks a hub-signed prepaid ticket (constant-time MAC + expiry).
func VerifyHubPrepaidTicket(hubSecret, ownerRef, campaignID string, budgetHMC float64, budgetRuns int, escrowFP string, got string, nowUnix int64) bool {
	hubSecret = strings.TrimSpace(hubSecret)
	escrowFP = strings.ToLower(strings.TrimSpace(escrowFP))
	if hubSecret == "" || escrowFP == "" {
		return false
	}
	expUnix, macHex, ok := ParseHubPrepaidTicket(got)
	if !ok {
		return false
	}
	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}
	if expUnix < nowUnix {
		return false
	}
	// Reject absurdly long-lived tickets (mis-issue / clock skew abuse).
	if expUnix-nowUnix > int64(DefaultHubPrepaidTicketTTL)+300 {
		return false
	}
	want, err := hex.DecodeString(strings.TrimSpace(macHex))
	if err != nil || len(want) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, HubPrepaidTicketMACKey(hubSecret))
	_, _ = mac.Write([]byte(CanonicalHubPrepaidTicket(ownerRef, campaignID, budgetHMC, budgetRuns, escrowFP, expUnix)))
	return hmac.Equal(want, mac.Sum(nil))
}

// VerifyPublishPrepaidAttestation verifies publish prepaid proof.
// Minting-hub / REQUIRE_HUB mode: hub ticket only (self-MAC refused).
// Lab: hub ticket if presented, else publish-token self-MAC.
func VerifyPublishPrepaidAttestation(publishToken, hubSecret, ownerRef, campaignID string, budgetHMC float64, budgetRuns int, escrowFP, got string, nowUnix int64, requireHub bool) bool {
	got = strings.TrimSpace(got)
	if got == "" {
		return false
	}
	if requireHub || IsHubPrepaidTicket(got) {
		if requireHub && !IsHubPrepaidTicket(got) {
			return false
		}
		return VerifyHubPrepaidTicket(hubSecret, ownerRef, campaignID, budgetHMC, budgetRuns, escrowFP, got, nowUnix)
	}
	return VerifyPrepaidAttestation(publishToken, ownerRef, campaignID, budgetHMC, budgetRuns, got)
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
