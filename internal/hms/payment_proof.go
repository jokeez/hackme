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

// MarketPaymentHMACSecret returns the shared secret used to attest node wallet debits
// to the HMS coordinator. Prefer HMS_MARKET_PAYMENT_HMAC_SECRET; fall back to coordinator token.
func MarketPaymentHMACSecret() string {
	for _, k := range []string{
		"HMS_MARKET_PAYMENT_HMAC_SECRET",
		"HACKME_HMS_COORDINATOR_TOKEN",
		"HMS_COORDINATOR_ADMIN_TOKEN",
		"HACKME_ADMIN_TOKEN",
	} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// SignMarketPaymentProof binds payment_id + quote_hash + debit amount.
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
	msg := fmt.Sprintf("%s|%s|%.8f", paymentID, quoteHash, totalDebitHMC)
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
// Callers must also enforce loopback (HTTP RemoteAddr or bind) separately.
func PilotPaymentSkipAllowed() bool {
	if strings.TrimSpace(os.Getenv("HMS_MARKET_SKIP_PAYMENT")) != "1" {
		return false
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HMS_COORDINATOR_ALLOW_INSECURE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
