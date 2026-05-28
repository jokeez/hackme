package hms

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// StorageSubmitPayload is signed by storage workers on proof submit.
type StorageSubmitPayload struct {
	WorkerID    string `json:"worker_id"`
	ChallengeID string `json:"challenge_id"`
	EpochID     int64  `json:"epoch_id"`
	ProofHex    string `json:"proof_hex"`
}

// SealSubmitPayload is signed by seal workers (CPU or ASIC gateway).
type SealSubmitPayload struct {
	WorkerID string `json:"worker_id"`
	EpochID  int64  `json:"epoch_id"`
	Nonce    uint64 `json:"nonce"`
}

func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func verifyEd25519(pubHex string, body []byte, sigHex string) error {
	pub, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid worker pubkey")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return errors.New("signature verify failed")
	}
	return nil
}

func VerifyStorageSubmit(p StorageSubmitPayload, pubHex, sigHex string) error {
	p.WorkerID = strings.TrimSpace(p.WorkerID)
	p.ChallengeID = strings.TrimSpace(p.ChallengeID)
	b, err := canonicalJSON(p)
	if err != nil {
		return err
	}
	return verifyEd25519(pubHex, b, sigHex)
}

func VerifySealSubmit(p SealSubmitPayload, pubHex, sigHex string) error {
	p.WorkerID = strings.TrimSpace(p.WorkerID)
	b, err := canonicalJSON(p)
	if err != nil {
		return err
	}
	return verifyEd25519(pubHex, b, sigHex)
}
