package block

// Task is a Proof-of-Hack challenge payload (MVP: metadata + opaque bytes).
type Task struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Payload    []byte `json:"payload,omitempty"`
	TargetHint string `json:"target_hint,omitempty"`
}

// Block is a single chain link (header fields + hash).
type Block struct {
	Index        uint64 `json:"index"`
	Timestamp    int64  `json:"timestamp_unix"`
	PrevHash     string `json:"prev_hash"`
	Hash         string `json:"hash"`
	MinerSigAlg  string `json:"miner_sig_alg,omitempty"`
	MinerPubKey  string `json:"miner_pubkey_ed25519,omitempty"`
	MinerSig     string `json:"miner_sig_ed25519,omitempty"`
	Nonce        uint64 `json:"nonce"`
	MinerAddress string `json:"miner_address"`
	Task         Task   `json:"task"`
}
