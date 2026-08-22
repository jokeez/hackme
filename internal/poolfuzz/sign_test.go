package poolfuzz

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestSubmitSignPayloadIncludesSegmentExecDone(t *testing.T) {
	p := SubmitSignPayload{
		WorkerID: "w1", CampaignID: "c1", ItemID: 7, InputN: 3,
		ActualInput: 42, CheckResult: 1, SubmitNonce: 99, SegmentExecDone: 512,
	}
	body := string(CanonicalSubmitBytes(p))
	if !strings.Contains(body, `"segment_exec_done":512`) {
		t.Fatalf("signature body must bind segment exec: %s", body)
	}
}

func TestSubmitSignPayloadIncludesInputBytes(t *testing.T) {
	p := SubmitSignPayload{
		WorkerID: "w1", CampaignID: "c1", ItemID: 7, InputN: 3,
		ActualInput: 42, InputBytesHex: "414b4941", CheckResult: 1, SubmitNonce: 99,
	}
	body := string(CanonicalSubmitBytes(p))
	if !strings.Contains(body, `"input_bytes_hex":"414b4941"`) {
		t.Fatalf("signature body must bind input bytes: %s", body)
	}
	p2 := SubmitSignPayload{
		WorkerID: "w1", CampaignID: "c1", ItemID: 7, InputN: 3,
		ActualInput: 42, CheckResult: 1, SubmitNonce: 99,
	}
	body2 := string(CanonicalSubmitBytes(p2))
	if strings.Contains(body2, "input_bytes_hex") {
		t.Fatalf("u64 mode should omit input_bytes_hex: %s", body2)
	}
	_ = hex.EncodeToString
}
