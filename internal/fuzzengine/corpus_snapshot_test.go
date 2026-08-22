package fuzzengine

import "testing"

func TestCorpusSnapshotRoundTrip(t *testing.T) {
	seeds := []PoolCorpusSeed{
		{Input: 42, InputBytes: []byte("GITHUB_PAT=ghp_test"), Energy: 3, Edge: 7, Path: 11},
		{Input: 99, InputBytes: []byte{0, 1, 2}, Energy: 1, Edge: 2, Path: 3},
	}
	b, sha, err := EncodeCorpusSnapshot(seeds)
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" || len(b) == 0 {
		t.Fatal("expected snapshot bytes")
	}
	got, err := DecodeCorpusSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(seeds) {
		t.Fatalf("len=%d want %d", len(got), len(seeds))
	}
	if string(got[0].InputBytes) != string(seeds[0].InputBytes) {
		t.Fatalf("bytes mismatch")
	}
	maps := CorpusSeedsClaimMaps(seeds)
	back, err := CorpusSeedsFromClaimMaps(maps)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(seeds) || back[0].Input != seeds[0].Input {
		t.Fatalf("claim maps roundtrip failed")
	}
}
