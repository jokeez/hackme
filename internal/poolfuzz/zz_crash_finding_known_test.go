package poolfuzz

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
)

func TestCrashFindingAlreadyKnownBucketAndBytes(t *testing.T) {
	svc, id, _ := researchDeltaHuntCamp(t)
	ctx := context.Background()
	now := time.Now().Unix()
	const trap = "hunt_crash:heap-buffer-overflow"
	key := fuzzengine.StableCrashBucket("native_crash", trap)
	if key == "" || key == "crash|generic" {
		t.Fatalf("unexpected bucket %q", key)
	}
	detail, _ := json.Marshal(map[string]any{"trap": trap, "stable_crash_key": key})
	_, err := svc.DB.ExecContext(ctx,
		`INSERT INTO fuzz_findings(id, campaign_id, finding_type, severity, title, input_sha256, detail_json, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"f-bucket-1", id, "native_crash", "high", "t", "aabbccdd", string(detail), now)
	if err != nil {
		t.Fatal(err)
	}

	// Different bytes, same trap → known via bucket.
	mut := SubmitRequest{CampaignID: id, Trap: trap, InputBytes: []byte("AAAAAAABCrash!"), ActualInput: 2}
	known, err := svc.crashFindingAlreadyKnown(ctx, id, mut)
	if err != nil {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("same-trap mutant must be known via StableCrashBucket")
	}

	// Empty trap is not gated as a crash novelty check.
	known, err = svc.crashFindingAlreadyKnown(ctx, id, SubmitRequest{CampaignID: id, Trap: ""})
	if err != nil || known {
		t.Fatalf("empty trap must be novel: known=%v err=%v", known, err)
	}
}
