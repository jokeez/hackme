package hunt

import (
	"bytes"
	"testing"
)

func TestShardAnchorBytesDeterministic(t *testing.T) {
	cfg := map[string]any{"upstream_target_id": "yyjson", "max_input_bytes": 128}
	a := ShardAnchorBytes("camp-1", 3, cfg)
	b := ShardAnchorBytes("camp-1", 3, cfg)
	if len(a) != 128 {
		t.Fatalf("len=%d want 128", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Fatal("not deterministic")
	}
	c := ShardAnchorBytes("camp-1", 4, cfg)
	if bytes.Equal(a, c) {
		t.Fatal("different input_n should differ")
	}
}

func TestShardSegmentExecInputMutatesAfterAnchor(t *testing.T) {
	cfg := map[string]any{
		"upstream_target_id":   "yyjson",
		"max_input_bytes":      128,
		"iterations_per_shard": 8,
		"depth_tier":           "oss_cve",
	}
	campaignID := "camp-mut"
	inputN := uint64(11)
	anchor := ShardSegmentExecInput(campaignID, inputN, 0, cfg)
	if !bytes.Equal(anchor, ShardAnchorBytes(campaignID, inputN, cfg)) {
		t.Fatal("exec 0 should be anchor")
	}
	mut := ShardSegmentExecInput(campaignID, inputN, 3, cfg)
	if bytes.Equal(anchor, mut) {
		t.Fatal("exec 3 should differ from anchor")
	}
	a := ShardSegmentExecInput(campaignID, inputN, 5, cfg)
	b := ShardSegmentExecInput(campaignID, inputN, 5, cfg)
	if !bytes.Equal(a, b) {
		t.Fatal("segment input not deterministic")
	}
}

func TestShardSegmentMutatingDisabledForSingleExec(t *testing.T) {
	cfg := map[string]any{
		"upstream_target_id":   "x",
		"iterations_per_shard": 1,
		"max_input_bytes":      64,
	}
	if ShardSegmentMutating(cfg) {
		t.Fatal("single exec should not mutate")
	}
	anchor := ShardSegmentExecInput("c", 1, 0, cfg)
	later := ShardSegmentExecInput("c", 1, 0, cfg)
	if !bytes.Equal(anchor, later) {
		t.Fatal("exec 0 stable")
	}
}
