package hms

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// MarketPaymentHMACSecret returns the dedicated secret used to attest node wallet debits
// to the HMS coordinator. Only HMS_MARKET_PAYMENT_HMAC_SECRET is accepted (no admin/worker
// token fallback — those would couple wallet-spend attestation to unrelated secrets).
func MarketPaymentHMACSecret() string {
	return strings.TrimSpace(os.Getenv("HMS_MARKET_PAYMENT_HMAC_SECRET"))
}

// MarketCoordinatorID binds payment proofs to a single HMS coordinator (HMC-RES-02).
// Empty means "local" — still included in the HMAC so proofs are not portable blindly.
func MarketCoordinatorID() string {
	for _, k := range []string{
		"HMS_MARKET_COORDINATOR_ID",
		"HACKME_HMS_COORDINATOR_ID",
		"HMS_COORDINATOR_ID",
	} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return "local"
}

// SignMarketPaymentProof binds coordinator_id + payment_id + quote_hash + debit amount.
func SignMarketPaymentProof(paymentID, quoteHash string, totalDebitHMC float64) (string, error) {
	secret := MarketPaymentHMACSecret()
	if secret == "" {
		return "", errors.New("hms: payment HMAC secret not configured")
	}
	paymentID = strings.TrimSpace(paymentID)
	quoteHash = strings.TrimSpace(strings.ToLower(quoteHash))
	if paymentID == "" || quoteHash == "" {
		return "", errors.New("hms: payment_id and quote_hash required for proof")
	}
	coord := MarketCoordinatorID()
	msg := fmt.Sprintf("%s|%s|%s|%.8f", coord, paymentID, quoteHash, totalDebitHMC)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyMarketPaymentProof rejects forged or mismatched payment attestations.
func VerifyMarketPaymentProof(paymentID, quoteHash, proof string, totalDebitHMC float64) error {
	want, err := SignMarketPaymentProof(paymentID, quoteHash, totalDebitHMC)
	if err != nil {
		return err
	}
	got := strings.TrimSpace(strings.ToLower(proof))
	if got == "" {
		return errors.New("payment_proof required")
	}
	if !hmac.Equal([]byte(got), []byte(strings.ToLower(want))) {
		return errors.New("payment_proof invalid")
	}
	return nil
}

// PilotPaymentSkipAllowed is true only when explicit insecure pilot mode is enabled.
// Callers must ALSO pass allowInsecurePilot=true only for loopback RemoteAddr (never XFF).
func PilotPaymentSkipAllowed() bool {
	if strings.TrimSpace(os.Getenv("HMS_MARKET_SKIP_PAYMENT")) != "1" {
		return false
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HMS_COORDINATOR_ALLOW_INSECURE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
