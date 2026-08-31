package hunt

import (
	"bytes"
	"testing"

	"hackme/internal/fuzzengine"
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
	anchor := ShardSegmentExecInput(campaignID, inputN, 0, cfg, nil)
	if !bytes.Equal(anchor, ShardAnchorBytes(campaignID, inputN, cfg)) {
		t.Fatal("exec 0 should be anchor")
	}
	mut := ShardSegmentExecInput(campaignID, inputN, 3, cfg, nil)
	if bytes.Equal(anchor, mut) {
		t.Fatal("exec 3 should differ from anchor")
	}
	a := ShardSegmentExecInput(campaignID, inputN, 5, cfg, nil)
	b := ShardSegmentExecInput(campaignID, inputN, 5, cfg, nil)
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
	anchor := ShardSegmentExecInput("c", 1, 0, cfg, nil)
	later := ShardSegmentExecInput("c", 1, 0, cfg, nil)
	if !bytes.Equal(anchor, later) {
		t.Fatal("exec 0 stable")
	}
}

func TestShardSegmentExecInputUsesCorpusAtExecZero(t *testing.T) {
	cfg := map[string]any{
		"upstream_target_id":   "yyjson",
		"max_input_bytes":      128,
		"iterations_per_shard": 8,
		"depth_tier":           "oss_cve",
		"hunt_corpus_guided":   true,
		"guided_scheduling":    true,
		"input_mode":           "bytes",
	}
	seeds := []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("seed-alpha-bytes-here-for-corpus-guided-hunt-shard-input-test-case-0123456789abcdef"), Energy: 4},
		{InputBytes: []byte("seed-beta-bytes-here-for-corpus-guided-hunt-shard-input-test-case-0123456789abcdef0"), Energy: 2},
	}
	campaignID := "camp-corpus"
	inputN := uint64(3)
	anchor := ShardAnchorBytes(campaignID, inputN, cfg)
	guided := ShardSegmentExecInput(campaignID, inputN, 0, cfg, seeds)
	if bytes.Equal(anchor, guided) {
		t.Fatal("corpus-guided exec 0 should differ from raw shard anchor")
	}
	g2 := ShardSegmentExecInput(campaignID, inputN, 0, cfg, seeds)
	if !bytes.Equal(guided, g2) {
		t.Fatal("corpus-guided exec 0 not deterministic")
	}
}
