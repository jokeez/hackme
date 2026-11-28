package poolfuzz

import (
	"context"
	"encoding/json"
	"strings"
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
	known, err := svc.crashFindingAlreadyKnown(ctx, id, mut, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("same-trap mutant must be known via StableCrashBucket")
	}

	// Empty trap is not gated as a crash novelty check.
	known, err = svc.crashFindingAlreadyKnown(ctx, id, SubmitRequest{CampaignID: id, Trap: ""}, nil)
	if err != nil || known {
		t.Fatalf("empty trap must be novel: known=%v err=%v", known, err)
	}
}

func TestCrashFindingDifferentPCIsNovel(t *testing.T) {
	svc, id, _ := researchDeltaHuntCamp(t)
	ctx := context.Background()
	now := time.Now().Unix()
	const trapA = "hunt_crash:heap-buffer-overflow pc=+0x4c3a11"
	const trapB = "hunt_crash:heap-buffer-overflow pc=+0x51aa02"
	keyA := fuzzengine.StableCrashBucket("native_crash", trapA)
	keyB := fuzzengine.StableCrashBucket("native_crash", trapB)
	if keyA == keyB || !strings.Contains(keyA, "+0x4c3a11") || !strings.Contains(keyB, "+0x51aa02") {
		t.Fatalf("keys a=%q b=%q", keyA, keyB)
	}
	rawSame := "==9==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x10 at pc 0x20\nSUMMARY: AddressSanitizer: heap-buffer-overflow (/tmp/other/harness+0x4c3a11)\n"
	if fuzzengine.StableCrashBucket("native_crash", rawSame) != keyA {
		t.Fatal("raw symbolize=0 report must share the stamped trap bucket")
	}
	detail, _ := json.Marshal(map[string]any{"trap": trapA, "stable_crash_key": keyA})
	_, err := svc.DB.ExecContext(ctx,
		`INSERT INTO fuzz_findings(id, campaign_id, finding_type, severity, title, input_sha256, detail_json, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"f-pc-1", id, "native_crash", "critical", "t", "aabbccdd", string(detail), now)
	if err != nil {
		t.Fatal(err)
	}
	known, err := svc.crashFindingAlreadyKnown(ctx, id, SubmitRequest{CampaignID: id, Trap: trapA, InputBytes: []byte("same-bug-other-bytes")}, nil)
	if err != nil || !known {
		t.Fatalf("same PC must be known: known=%v err=%v", known, err)
	}
	known, err = svc.crashFindingAlreadyKnown(ctx, id, SubmitRequest{CampaignID: id, Trap: trapB, InputBytes: []byte("other-bug")}, nil)
	if err != nil || known {
		t.Fatalf("different PC must still pay: known=%v err=%v", known, err)
	}
}
