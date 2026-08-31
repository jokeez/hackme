package poolfuzz

import (
	"testing"

	"hackme/internal/fuzzescrow"
)

func TestIsHuntCampaign(t *testing.T) {
	if !IsHuntCampaign(map[string]any{"work_kind": "hunt_shard"}) {
		t.Fatal("work_kind hunt_shard")
	}
	if !IsHuntCampaign(map[string]any{"campaign_type": "hunt"}) {
		t.Fatal("campaign_type hunt")
	}
	if IsHuntCampaign(map[string]any{"work_kind": "wasm_segment"}) {
		t.Fatal("wasm should not be hunt")
	}
}

func TestHuntShardInputBytesDeterministic(t *testing.T) {
	cfg := map[string]any{"upstream_target_id": "yyjson", "max_input_bytes": 128}
	a := HuntShardInputBytes("camp-1", 3, cfg)
	b := HuntShardInputBytes("camp-1", 3, cfg)
	if len(a) != 128 {
		t.Fatalf("len=%d want 128", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("not deterministic")
		}
	}
	c := HuntShardInputBytes("camp-1", 4, cfg)
	if string(a) == string(c) {
		t.Fatal("different input_n should differ")
	}
}

func TestPerShardHMCFromConfig5050(t *testing.T) {
	cfg := map[string]any{
		"budget_hmc":   20.0,
		"budget_shards": 8,
		"escrow_split": fuzzescrow.EscrowSplit5050,
		"work_kind":    "hunt_shard",
	}
	got := perShardHMCFromConfig(cfg)
	want := (20.0 * 0.50) / 8.0
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("per shard got %v want %v", got, want)
	}
	if perRunHMCFromConfig(cfg) != got {
		t.Fatal("perRunHMCFromConfig should delegate to per shard for hunt")
	}
}

func TestEvalHuntSubmitCrash(t *testing.T) {
	s := &Service{}
	cfg := map[string]any{"iterations_per_shard": 32}
	req := SubmitRequest{SegmentExecDone: 32, CheckResult: 1, Trap: "hunt_crash:asan"}
	_, trap, pass, finding := s.evalHuntSubmit(cfg, req, []byte{1, 2, 3})
	if !pass || !finding || trap != "hunt_crash:asan" {
		t.Fatalf("crash submit pass=%v finding=%v trap=%q", pass, finding, trap)
	}
	req2 := SubmitRequest{SegmentExecDone: 31, Trap: "hunt_crash:asan", CheckResult: 1}
	_, _, pass2, finding2 := s.evalHuntSubmit(cfg, req2, nil)
	if pass2 || finding2 {
		t.Fatal("wrong exec count should fail")
	}
}
