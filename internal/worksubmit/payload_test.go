package worksubmit

import (
	"encoding/json"
	"testing"
)

func TestSignPayloadCanonicalJSONStable(t *testing.T) {
	p := SignPayload{
		WorkerID:    "w1",
		BaseNonce:   10,
		BatchSize:   4000000,
		WorkID:      "w1:10+4000000",
		Attempts:    4000000,
		Found:       false,
		FoundNonce:  0,
		ResultHash:  "",
		ProofHash:   "",
		SubmitNonce: 7,
	}
	got := string(p.CanonicalJSON())
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if m["worker_id"] != "w1" || m["submit_nonce"].(float64) != 7 {
		t.Fatalf("unexpected decode: %v", m)
	}
}
