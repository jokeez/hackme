package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/lanpool"
)

func TestHandleNetworkStatsRedactsWhenAdminTokenUnset(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	t.Setenv("HACKME_REQUIRE_ADMIN_TOKEN", "0")

	in := lanpool.NetworkStatsResponse{
		TotalMiners: 1,
		TopMiners:   []string{"HMC-pool-slot-1", "HMC-pool-slot-2"},
		ActiveRigs: []lanpool.NetworkRigRow{
			{MetricsRow: lanpool.MetricsRow{Name: "r1", IP: "10.0.0.2"}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/network/stats", nil)
	out := in
	// Mirror handleNetworkStats: redact unless admin auth is both enabled AND satisfied.
	if !(adminAuthEnabled() && adminRequestAuthed(req)) {
		out = lanpool.RedactPublicNetworkStats(in)
	}
	if len(out.TopMiners) != 0 {
		t.Fatalf("top_miners must be redacted when admin token unset, got %#v", out.TopMiners)
	}
	if len(out.ActiveRigs) != 1 || out.ActiveRigs[0].IP != "" {
		b, _ := json.Marshal(out.ActiveRigs)
		t.Fatalf("active_rigs IP must be blanked: %s", b)
	}
}
