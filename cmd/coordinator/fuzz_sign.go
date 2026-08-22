package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
	"strings"
)

type fuzzSubmitAuth struct {
	WorkerID     string
	MinerAddress string
	MinerPubKey  string
	MinerSig     string
	MinerSigAlg  string
	SubmitNonce  uint64
}

// validateFuzzHybridSignature binds miner_address to ed25519 proof-of-possession (same model as PoH submit).
// Any non-empty miner_address for escrow payout requires hybrid PoP (B4 fail-closed).
// M1: signature checks only — nonce is committed via commitFuzzHybridNonce after work accept.
func (m *workManager) validateFuzzHybridSignature(auth fuzzSubmitAuth, body []byte) (ok bool, reason string, payoutAddr string) {
	addr := strings.TrimSpace(auth.MinerAddress)
	if addr != "" && (!strings.HasPrefix(addr, "HMC-") || len(addr) != 20) {
		return false, "invalid_miner_address", ""
	}
	if m == nil || !m.hybridSignerEnabled {
		if addr != "" {
			return false, "hybrid_required_for_payout", ""
		}
		return true, "", ""
	}
	pubHex := strings.TrimSpace(auth.MinerPubKey)
	sigHex := strings.TrimSpace(auth.MinerSig)
	hasSig := !(pubHex == "" && sigHex == "" && auth.SubmitNonce == 0)
	if !hasSig {
		// Unsigned payout address is never allowed (even when STRICT=0).
		if addr != "" || m.hybridSignerStrict {
			return false, "signature_required", ""
		}
		return true, "", ""
	}
	if pubHex == "" || sigHex == "" || auth.SubmitNonce == 0 {
		return false, "missing_signature_fields", ""
	}
	alg := strings.TrimSpace(strings.ToLower(auth.MinerSigAlg))
	if alg == "" {
		alg = "ed25519"
	}
	if alg != "ed25519" {
		return false, "unsupported_sig_alg", ""
	}
	derived, okAddr := deriveAddressFromPubHex(pubHex)
	if !okAddr {
		return false, "invalid_pubkey", ""
	}
	if addr != "" && !strings.EqualFold(addr, derived) {
		return false, "pubkey_address_mismatch", ""
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, "invalid_signature", ""
	}
	pub, _ := hex.DecodeString(pubHex)
	if !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return false, "invalid_signature", ""
	}
	// Same mutex as PoH submit — concurrent map writes without it panic under load.
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxNonce, ok := m.signedSubmitNonceMax[derived]; ok && auth.SubmitNonce <= maxNonce {
		return false, "replay", ""
	}
	nonceKey := derived + ":fuzz:" + strconv.FormatUint(auth.SubmitNonce, 10)
	if _, exists := m.acceptedSubmitNonces[nonceKey]; exists {
		return false, "replay", ""
	}
	if len(m.acceptedSubmitNonces) >= m.maxDedupEntries && m.maxDedupEntries > 0 {
		for k := range m.acceptedSubmitNonces {
			delete(m.acceptedSubmitNonces, k)
			break
		}
	}
	m.acceptedSubmitNonces[nonceKey] = struct{}{}
	if auth.SubmitNonce > m.signedSubmitNonceMax[derived] {
		m.signedSubmitNonceMax[derived] = auth.SubmitNonce
	}
	return true, "", derived
}

// commitFuzzHybridNonce records a fuzz submit nonce after the work item was accepted.
func (m *workManager) commitFuzzHybridNonce(derived string, submitNonce uint64) {
	if m == nil || derived == "" || submitNonce == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acceptedSubmitNonces == nil {
		m.acceptedSubmitNonces = make(map[string]struct{})
	}
	if m.signedSubmitNonceMax == nil {
		m.signedSubmitNonceMax = make(map[string]uint64)
	}
	nonceKey := derived + ":fuzz:" + strconv.FormatUint(submitNonce, 10)
	if len(m.acceptedSubmitNonces) >= m.maxDedupEntries && m.maxDedupEntries > 0 {
		for k := range m.acceptedSubmitNonces {
			delete(m.acceptedSubmitNonces, k)
			break
		}
	}
	m.acceptedSubmitNonces[nonceKey] = struct{}{}
	if submitNonce > m.signedSubmitNonceMax[derived] {
		m.signedSubmitNonceMax[derived] = submitNonce
	}
	m.lastSignedMiner = derived
}
