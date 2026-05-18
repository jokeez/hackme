package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRPGItemsCheckProbe(t *testing.T) {
	path := filepath.Join("..", "..", "logs", "desktop", "rpg_items_check.wasm")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("wasm artifact missing:", err)
	}
	if err := ValidateCheckWasm(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		n    uint64
	}{
		{"bug1_oob_item99", 1 | (99 << 8)},
		{"bug2_div0", 2},
		{"bug3_overflow", 3 | (5000000 << 24)},
		{"ok_buy_small", 3 | (10 << 24)},
	}
	for _, c := range cases {
		ok, err := InvokeCheck(context.Background(), raw, c.n)
		t.Logf("%s n=%d ok=%v err=%v", c.name, c.n, ok, err)
	}
}
