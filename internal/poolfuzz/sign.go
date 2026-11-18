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
	DurationMS      int    `json:"duration_ms,omitempty"`
	EdgesTouched    int    `json:"edges_touched,omitempty"`
	EdgesTouchedOK  bool   `json:"edges_touched_ok,omitempty"`
}

// CanonicalSubmitBytes returns deterministic JSON for hybrid signature verification.
func CanonicalSubmitBytes(p SubmitSignPayload) []byte {
	p.WorkerID = strings.TrimSpace(p.WorkerID)
	p.CampaignID = strings.TrimSpace(p.CampaignID)
	b, _ := json.Marshal(p)
	return b
}

// CanonicalSubmitBytesLegacy omits duration/edges (pre-hygiene-floor binding).
// Used only as a one-release rolling-deploy fallback while workers upgrade.
func CanonicalSubmitBytesLegacy(p SubmitSignPayload) []byte {
	p.DurationMS = 0
	p.EdgesTouched = 0
	p.EdgesTouchedOK = false
	return CanonicalSubmitBytes(p)
}

// ReleaseSignPayload is the canonical body for ed25519 fuzz lease-release PoP
// (AZ 2026-11-15: unsigned pubkey alone must not free another worker's lease).
type ReleaseSignPayload struct {
	WorkerID     string `json:"worker_id"`
	CampaignID   string `json:"campaign_id"`
	ItemID       int64  `json:"item_id"`
	ReleaseNonce uint64 `json:"release_nonce"`
}

// CanonicalReleaseBytes returns deterministic JSON for hybrid release signatures.
func CanonicalReleaseBytes(p ReleaseSignPayload) []byte {
	p.WorkerID = strings.TrimSpace(p.WorkerID)
	p.CampaignID = strings.TrimSpace(p.CampaignID)
	b, _ := json.Marshal(p)
	return b
}
