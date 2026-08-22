package poolfuzz

import (
	"encoding/json"
	"strings"
)

// SubmitSignPayload is the canonical body for ed25519 fuzz work submit signatures.
type SubmitSignPayload struct {
	WorkerID        string `json:"worker_id"`
	CampaignID      string `json:"campaign_id"`
	ItemID          int64  `json:"item_id"`
	InputN          uint64 `json:"input_n"`
	ActualInput     uint64 `json:"actual_input"`
	InputBytesHex   string `json:"input_bytes_hex,omitempty"`
	CheckResult     int32  `json:"check_result"`
	SubmitNonce     uint64 `json:"submit_nonce"`
	SegmentExecDone int    `json:"segment_exec_done,omitempty"`
}

// CanonicalSubmitBytes returns deterministic JSON for hybrid signature verification.
func CanonicalSubmitBytes(p SubmitSignPayload) []byte {
	p.WorkerID = strings.TrimSpace(p.WorkerID)
	p.CampaignID = strings.TrimSpace(p.CampaignID)
	b, _ := json.Marshal(p)
	return b
}
