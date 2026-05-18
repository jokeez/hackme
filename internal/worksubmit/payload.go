package worksubmit

import (
	"encoding/json"
	"strings"
)

// SignPayload is the exact hybrid-signer message shape shared between the coordinator
// and offline minersign tooling. Field order matches encoding/json struct marshaling.
type SignPayload struct {
	WorkerID    string `json:"worker_id"`
	BaseNonce   uint64 `json:"base_nonce"`
	BatchSize   uint64 `json:"batch_size"`
	WorkID      string `json:"work_id"`
	Attempts    uint64 `json:"attempts"`
	Found       bool   `json:"found"`
	FoundNonce  uint64 `json:"found_nonce"`
	ResultHash  string `json:"result_hash"`
	ProofHash   string `json:"proof_hash"`
	SubmitNonce uint64 `json:"submit_nonce"`
}

// NormalizeHashes matches coordinator hybrid normalization for hashes embedded in the payload.
func NormalizeHashes(resultHash, proofHash string) (string, string) {
	return strings.TrimSpace(strings.ToLower(resultHash)), strings.TrimSpace(strings.ToLower(proofHash))
}

// CanonicalJSON returns the byte-for-byte canonical signing material for Ed25519.
func (p SignPayload) CanonicalJSON() []byte {
	raw, _ := json.Marshal(p)
	return raw
}
