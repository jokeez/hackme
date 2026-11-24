package main

import (
	"context"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

// A pinned catalog harness_hash is derived from the manifest entry, not from the
// built bytes, which is what lets a node re-materialise a lost harness under the
// exact same id. These two hashes are the ones that sat stuck on the hub with
// 769.5 HMC of escrow locked behind them, so pin them: if a manifest edit moves
// them, every live campaign that references them stops resolving.
func TestCatalogHarnessHashIsStableForStuckTargets(t *testing.T) {
	want := map[string]string{
		"duktape": "de2b275b7f165d679bdcf5722626311a",
		"libucl":  "aa2ff1b38256d7fe47a1e74e2dd72a67",
	}
	for id, exp := range want {
		got, err := hunt.CatalogHarnessHash(".", id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got != exp {
			t.Fatalf("%s harness hash changed: %s != %s — live campaigns pin the old id", id, got, exp)
		}
	}
}

func TestRebuildCatalogHarnessRefusesNonCatalogCampaigns(t *testing.T) {
	a := &app{}
	ctx := context.Background()

	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{
			name: "inventory source is content-bound",
			cfg: map[string]any{
				"hunt_source":        "inventory",
				"hunt_source_rel":    "tasks/sources/fuzz/oss/duktape_stdin.c",
				"upstream_target_id": "duktape",
			},
			want: "not a pinned catalog campaign",
		},
		{
			name: "customer repo harness",
			cfg: map[string]any{
				"hunt_source":     "customer_repo",
				"hunt_source_rel": "harness.c",
			},
			want: "not a pinned catalog campaign",
		},
		{
			name: "catalog without a target id",
			cfg:  map[string]any{"hunt_source": "catalog"},
			want: "not a pinned catalog campaign",
		},
		{
			name: "nil config",
			cfg:  nil,
			want: "not a pinned catalog campaign",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := a.rebuildCatalogHarnessArtifact(ctx, tc.cfg, "de2b275b7f165d679bdcf5722626311a")
			if err == nil {
				t.Fatalf("rebuild must be refused, got %d bytes", len(data))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The rebuild must not accept a target whose manifest hash does not match the
// campaign's harness_hash: that would rebind a live id to different sources.
func TestRebuildCatalogHarnessRefusesHashMismatch(t *testing.T) {
	a := &app{}
	cfg := map[string]any{"hunt_source": "catalog", "upstream_target_id": "duktape"}
	if _, err := a.rebuildCatalogHarnessArtifact(context.Background(), cfg, "ffffffffffffffffffffffffffffffff"); err == nil {
		t.Fatal("rebuild accepted a harness_hash that the catalog target does not produce")
	}
}
