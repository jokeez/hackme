package fuzzengine

import "testing"

func TestParseMaxInputBytesClamps(t *testing.T) {
	cfg := map[string]any{"max_input_bytes": 99999, "input_mode": "bytes"}
	if got := ParseMaxInputBytes(cfg); got != MaxInputBytesHardCeil {
		t.Fatalf("ceil: got %d want %d", got, MaxInputBytesHardCeil)
	}
	cfg["max_input_bytes"] = 2
	if got := ParseMaxInputBytes(cfg); got != MinMaxInputBytes {
		t.Fatalf("floor: got %d want %d", got, MinMaxInputBytes)
	}
}

func TestClampInputBytes(t *testing.T) {
	cfg := map[string]any{"max_input_bytes": 16, "input_mode": "bytes"}
	in := make([]byte, 32)
	for i := range in {
		in[i] = byte(i)
	}
	out := ClampInputBytes(in, cfg)
	if len(out) != 16 {
		t.Fatalf("len=%d want 16", len(out))
	}
}

func TestByteTierPreset(t *testing.T) {
	if ByteTierPreset("4k") != 4096 {
		t.Fatal("4k preset")
	}
	if ByteTierPreset("lite") != 256 {
		t.Fatal("lite preset")
	}
}
