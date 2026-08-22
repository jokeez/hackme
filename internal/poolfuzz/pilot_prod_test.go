package poolfuzz

import "testing"

func TestProdOptInBytesPilotConfig(t *testing.T) {
	cfg := ProdOptInBytesPilotConfig("abcd", 1024, nil)
	if cfg["input_mode"] != "bytes" {
		t.Fatalf("input_mode=%v", cfg["input_mode"])
	}
	if cfg["pilot"] != ProdBytesPilotFlag {
		t.Fatalf("pilot=%v", cfg["pilot"])
	}
	if !IsBytesPilotCampaign(cfg) {
		t.Fatal("expected bytes pilot campaign")
	}
	if IsBytesPilotCampaign(map[string]any{"input_mode": "bytes"}) {
		t.Fatal("pilot flag required")
	}
}
