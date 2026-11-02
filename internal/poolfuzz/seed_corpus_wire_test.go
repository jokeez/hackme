package poolfuzz

import (
	"strings"
	"testing"
)

func TestTrimSeedByteCorpusForWireCapsItems(t *testing.T) {
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_ITEMS", "3")
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_BYTES", "100000")
	in := []any{"a", "b", "c", "d", "e"}
	out := TrimSeedByteCorpusForWire(in)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	if out[0] != "a" || out[2] != "c" {
		t.Fatalf("order broken: %#v", out)
	}
}

func TestTrimSeedByteCorpusForWireCapsBytes(t *testing.T) {
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_ITEMS", "50")
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_BYTES", "64")
	// Three 40-byte seeds → first fits, second hits budget, third dropped.
	s40 := strings.Repeat("x", 40)
	in := []any{s40, s40, s40}
	out := TrimSeedByteCorpusForWire(in)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1 (40+40 would exceed 64)", len(out))
	}
}

func TestTrimSeedByteCorpusForWireClaimReplayParity(t *testing.T) {
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_ITEMS", "4")
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_BYTES", "10000")
	merged := []any{"s0", "s1", "s2", "s3", "s4", "s5"}
	claim := TrimSeedByteCorpusForWire(merged)
	replay := TrimSeedByteCorpusForWire(append([]any(nil), merged...))
	if len(claim) != len(replay) {
		t.Fatalf("len claim=%d replay=%d", len(claim), len(replay))
	}
	for i := range claim {
		if claim[i] != replay[i] {
			t.Fatalf("mismatch at %d: %v vs %v", i, claim[i], replay[i])
		}
	}
}

func TestTrimSeedByteCorpusForWireEmpty(t *testing.T) {
	if TrimSeedByteCorpusForWire(nil) != nil {
		t.Fatal("nil")
	}
	if len(TrimSeedByteCorpusForWire([]any{})) != 0 {
		t.Fatal("empty")
	}
}

func TestApplySeedByteCorpusWireTrimMutatesCfg(t *testing.T) {
	in := make([]any, 0, 80)
	for i := 0; i < 80; i++ {
		in = append(in, "41")
	}
	cfg := map[string]any{"seed_byte_corpus": in}
	applySeedByteCorpusWireTrim(cfg)
	list, _ := cfg["seed_byte_corpus"].([]any)
	if len(list) > defaultSeedCorpusWireMaxItems {
		t.Fatalf("len=%d want <=%d", len(list), defaultSeedCorpusWireMaxItems)
	}
	// Claim + submit must see identical trimmed lists.
	cfg2 := map[string]any{"seed_byte_corpus": append([]any(nil), in...)}
	applySeedByteCorpusWireTrim(cfg2)
	a, _ := cfg["seed_byte_corpus"].([]any)
	b, _ := cfg2["seed_byte_corpus"].([]any)
	if len(a) != len(b) {
		t.Fatalf("parity len %d vs %d", len(a), len(b))
	}
}
