package workerfuzzloop

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestCollectCoverageEngineSeedsMergesSeedByteCorpus(t *testing.T) {
	tlv := []byte{'H', 'W', 'I', 'R', 1, 0, 8, 0, 0x10, 6, 0, 4, 0, 'A', 'B', 'C', 'D'}
	anchor := []byte{0x00}
	cr := ClaimResp{
		InputBytesHex: hex.EncodeToString(anchor),
		CorpusSeeds: []map[string]any{
			{"input_bytes_hex": hex.EncodeToString([]byte{0x01, 0x02})},
		},
		SeedByteCorpus: []any{
			hex.EncodeToString(tlv),
			hex.EncodeToString(anchor), // dup of InputBytesHex — should dedupe
		},
	}
	got := collectCoverageEngineSeeds(cr)
	if len(got) < 3 {
		t.Fatalf("want >=3 unique seeds (anchor+corpus+tlv), got %d", len(got))
	}
	foundTLV := false
	for _, b := range got {
		if len(b) == len(tlv) && b[0] == 'H' && b[8] == 0x10 {
			foundTLV = true
		}
	}
	if !foundTLV {
		t.Fatal("seed_byte_corpus TLV pack seed missing from LF/AFL SeedFiles")
	}
}

func TestCollectCoverageEngineSeedsPackBeatsFatSnapshot(t *testing.T) {
	tlv := []byte{'H', 'W', 'I', 'R', 1, 0, 8, 0, 0x10, 6, 0, 4, 0, 'A', 'B', 'C', 'D'}
	seeds := make([]map[string]any, 0, 100)
	for i := 0; i < 100; i++ {
		seeds = append(seeds, map[string]any{"input_bytes_hex": hex.EncodeToString([]byte{byte(i), 0x11, 0x22})})
	}
	cr := ClaimResp{
		CorpusSeeds:    seeds,
		SeedByteCorpus: []any{hex.EncodeToString(tlv)},
	}
	got := collectCoverageEngineSeeds(cr)
	if len(got) == 0 || len(got) > 96 {
		t.Fatalf("len=%d", len(got))
	}
	if len(got[0]) != len(tlv) || got[0][0] != 'H' {
		t.Fatal("seed_byte_corpus must occupy the first LF seed slot")
	}
}

func TestCoverageEngineDictFallsBackToProfile(t *testing.T) {
	got := coverageEngineDict(ClaimResp{UpstreamTargetID: "jsmn"})
	if !strings.Contains(string(got), "\"null\"") {
		t.Fatalf("profile dict=%q", got)
	}
	explicit := coverageEngineDict(ClaimResp{
		UpstreamTargetID: "jsmn",
		LibFuzzerDictHex: hex.EncodeToString([]byte("kw=\"HWIR\"\n")),
	})
	if string(explicit) != "kw=\"HWIR\"\n" {
		t.Fatalf("explicit dict lost: %q", explicit)
	}
}

func TestHavocBurstKeepsLongV210Seed(t *testing.T) {
	pack := bytes.Repeat([]byte("A"), 8000)
	cr := ClaimResp{
		HavocDeepV210:  true,
		SeedByteCorpus: []any{hex.EncodeToString(pack)},
	}
	got := collectCoverageEngineSeeds(cr)
	if len(got) < 2 {
		t.Fatalf("len=%d", len(got))
	}
	if len(got[0]) != 8000 {
		t.Fatalf("head len=%d", len(got[0]))
	}
	// Old 4096 havoc cap would have chopped the mutant. v210 follows LF max_len.
	if len(got[1]) <= 4096 {
		t.Fatalf("v210 mutant len=%d want >4096", len(got[1]))
	}
}

func TestCollectCoverageEngineSeedsHavocBurstKeepsPackHead(t *testing.T) {
	pack := []byte("PACK-SEED-HEAD")
	cr := ClaimResp{
		HavocDeepV210:  true,
		SeedByteCorpus: []any{hex.EncodeToString(pack)},
	}
	got := collectCoverageEngineSeeds(cr)
	if len(got) < 2 {
		t.Fatalf("want pack + havoc mutant, got %d", len(got))
	}
	if string(got[0]) != string(pack) {
		t.Fatalf("head=%q", got[0])
	}
}

func TestCorpusSeedBytesRawNotHex(t *testing.T) {
	b, ok := corpusSeedBytes(map[string]any{"input_bytes": []byte{0x01, 0x02}})
	if !ok || len(b) != 2 || b[0] != 0x01 {
		t.Fatalf("raw bytes %#v ok=%v", b, ok)
	}
	b, ok = corpusSeedBytes(map[string]any{"input_bytes": "HWIR"})
	if !ok || string(b) != "HWIR" {
		t.Fatalf("raw string %#v ok=%v", b, ok)
	}
}

func TestCollectCoverageEngineSeedsLoadsAcceptedUnitWithoutSnapshot(t *testing.T) {
	fresh := []byte("feature-unit-from-miner-a")
	fillers := make([]map[string]any, 0, 96)
	for i := 0; i < 96; i++ {
		fillers = append(fillers, map[string]any{
			"input_bytes_hex": hex.EncodeToString([]byte{0xB0, byte(i), 0x11}),
		})
	}
	// CorpusLight means the fat snapshot was omitted. The claim's seed_byte_corpus
	// head is miner A's unit; it must occupy the first engine file anyway.
	got := collectCoverageEngineSeeds(ClaimResp{
		SeedByteCorpus: []any{hex.EncodeToString(fresh)},
		CorpusSeeds:    fillers,
		CorpusLight:    true,
	})
	if len(got) == 0 || !bytes.Equal(got[0], fresh) {
		t.Fatalf("engine seeds head=%q len=%d", got, len(got))
	}
}

func TestCollectCoverageEngineSeedsEmpty(t *testing.T) {
	if got := collectCoverageEngineSeeds(ClaimResp{}); len(got) != 0 {
		t.Fatalf("empty claim should yield 0 seeds, got %d", len(got))
	}
}
