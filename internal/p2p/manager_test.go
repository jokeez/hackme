package p2p

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/block"
)

func TestFailureBackoffSec(t *testing.T) {
	cases := []struct {
		fails uint64
		want  int64
	}{
		{fails: 0, want: 0},
		{fails: 1, want: 5},
		{fails: 2, want: 5},
		{fails: 3, want: 5},
		{fails: 4, want: 8},
		{fails: 8, want: 120},
		{fails: 20, want: 120},
	}
	for _, tc := range cases {
		got := failureBackoffSec(tc.fails)
		if got != tc.want {
			t.Fatalf("fails=%d: want %d, got %d", tc.fails, tc.want, got)
		}
	}
}

func TestPeerStatusQualityAndUnstable(t *testing.T) {
	st := PeerStatus{}
	st.updateQuality()
	if st.Quality != "unknown" || st.QualityScore != 0 || st.Unstable {
		t.Fatalf("initial quality mismatch: quality=%q score=%d unstable=%v", st.Quality, st.QualityScore, st.Unstable)
	}

	st.LastAttemptUnix = 100
	st.applyHealthTransition(true, 100)
	st.LastLatencyMS = 120
	st.ConsecutiveFailures = 0
	st.updateQuality()
	if st.Quality != "good" {
		t.Fatalf("expected good quality, got %q (score=%d)", st.Quality, st.QualityScore)
	}

	st.applyHealthTransition(false, 110)
	st.ConsecutiveFailures = 3
	st.LastLatencyMS = 2500
	st.updateQuality()
	if st.Quality != "bad" || !st.Unstable {
		t.Fatalf("expected bad/unstable, got quality=%q unstable=%v score=%d", st.Quality, st.Unstable, st.QualityScore)
	}
}

func TestLearnDiscoveredPeer(t *testing.T) {
	m := NewManager("n1", []string{"http://10.0.0.2:8080"}, "", true, "http://10.0.0.1:8080", "ph1")
	if !m.Enabled() || !m.DiscoveryEnabled() {
		t.Fatalf("manager should be enabled with discovery")
	}
	if ok := m.LearnDiscoveredPeer("http://10.0.0.3:8080"); !ok {
		t.Fatalf("expected discovered peer to be learned")
	}
	if ok := m.LearnDiscoveredPeer("http://10.0.0.3:8080"); ok {
		t.Fatalf("duplicate discovered peer must not be added")
	}
	if ok := m.LearnDiscoveredPeer("http://10.0.0.1:8080"); ok {
		t.Fatalf("self advertise url must not be added")
	}
	peers := m.PeerSnapshots()
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestNormalizePeerURL(t *testing.T) {
	if got := normalizePeerURL("http://10.0.0.2:8080/"); got != "http://10.0.0.2:8080" {
		t.Fatalf("unexpected normalized url: %q", got)
	}
	if got := normalizePeerURL("ftp://10.0.0.2:8080"); got != "" {
		t.Fatalf("expected unsupported scheme to be rejected, got %q", got)
	}
	if got := normalizePeerURL("not a url"); got != "" {
		t.Fatalf("expected invalid url to be rejected, got %q", got)
	}
}

func TestPolicyCompatible(t *testing.T) {
	cases := []struct {
		name   string
		local  string
		remote string
		want   bool
	}{
		{name: "local empty allows remote empty", local: "", remote: "", want: true},
		{name: "local empty allows remote value", local: "", remote: "abc", want: true},
		{name: "exact match", local: "abc123", remote: "abc123", want: true},
		{name: "case-insensitive match", local: "AbC123", remote: "aBc123", want: true},
		{name: "remote empty rejected when local set", local: "abc123", remote: "", want: false},
		{name: "mismatch rejected", local: "abc123", remote: "zzz999", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := policyCompatible(tc.local, tc.remote)
			if got != tc.want {
				t.Fatalf("policyCompatible(%q,%q): want %v, got %v", tc.local, tc.remote, tc.want, got)
			}
		})
	}
}

func TestLearnDiscoveredPeer_RespectsCap(t *testing.T) {
	m := NewManager("n1", []string{"http://10.0.0.2:8080"}, "", true, "", "ph1")
	m.maxPeers = 2
	if ok := m.LearnDiscoveredPeer("http://10.0.0.3:8080"); !ok {
		t.Fatalf("expected first discovered peer to be added")
	}
	if ok := m.LearnDiscoveredPeer("http://10.0.0.4:8080"); ok {
		t.Fatalf("expected discovery cap to reject additional peers")
	}
}

func TestMaxPeerCap(t *testing.T) {
	if got := maxPeerCap(0); got != 128 {
		t.Fatalf("seed=0: want 128, got %d", got)
	}
	if got := maxPeerCap(30); got != 240 {
		t.Fatalf("seed=30: want 240, got %d", got)
	}
	if got := maxPeerCap(200); got != 512 {
		t.Fatalf("seed=200: want 512, got %d", got)
	}
}

func TestShouldRelayTx_DeduplicatesWithinTTL(t *testing.T) {
	m := NewManager("n1", []string{"http://10.0.0.2:8080"}, "", false, "", "ph1")
	raw := []byte(`{"tx_type":"transfer_v1","nonce":1}`)
	now := int64(100)
	if ok := m.shouldRelayTx(raw, now); !ok {
		t.Fatalf("first relay must be allowed")
	}
	if ok := m.shouldRelayTx(raw, now+1); ok {
		t.Fatalf("duplicate relay in ttl window must be blocked")
	}
	if ok := m.shouldRelayTx(raw, now+121); !ok {
		t.Fatalf("relay must be allowed after ttl expiry")
	}
}

func TestBuildSyncHint(t *testing.T) {
	m := NewManager("n1", []string{"http://10.0.0.2:8080", "http://10.0.0.3:8080"}, "", false, "", "ph1")
	m.mu.Lock()
	m.peers["http://10.0.0.2:8080"] = PeerStatus{PeerURL: "http://10.0.0.2:8080", NodeID: "n2", Height: 10, Healthy: true}
	m.peers["http://10.0.0.3:8080"] = PeerStatus{PeerURL: "http://10.0.0.3:8080", NodeID: "n3", Height: 12, Healthy: true}
	m.mu.Unlock()
	h := m.BuildSyncHint(9, "h9")
	if !h.SyncNeeded || h.LagBlocks != 3 || h.BestPeerURL != "http://10.0.0.3:8080" {
		t.Fatalf("unexpected sync hint: %+v", h)
	}

	h2 := m.BuildSyncHint(12, "h12")
	if h2.SyncNeeded || h2.LagBlocks != 0 {
		t.Fatalf("expected no lag at tip, got %+v", h2)
	}
}

func TestBuildSyncHint_BootstrapFromGenesisPeer(t *testing.T) {
	m := NewManager("n1", []string{"http://10.0.0.2:8080"}, "", false, "", "ph1")
	m.mu.Lock()
	m.peers["http://10.0.0.2:8080"] = PeerStatus{
		PeerURL: "http://10.0.0.2:8080",
		NodeID:  "n2",
		Height:  0,
		Tip:     "genesis-hash",
		Healthy: true,
	}
	m.mu.Unlock()
	h := m.BuildSyncHint(0, "")
	if !h.SyncNeeded || h.LagBlocks != 1 || h.BestPeerURL != "http://10.0.0.2:8080" {
		t.Fatalf("bootstrap sync hint mismatch: %+v", h)
	}
}

func TestBuildLinearPullPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chain" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("from_height") != "4" {
			http.Error(w, "expected from_height=4", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blocks":[
			{"index":4,"hash":"h4","prev_hash":"h3"},
			{"index":5,"hash":"h5","prev_hash":"h4"},
			{"index":6,"hash":"h6","prev_hash":"h5"}
		]}`))
	}))
	defer srv.Close()

	m := NewManager("n1", []string{srv.URL}, "", false, "", "ph1")
	m.mu.Lock()
	m.peers[srv.URL] = PeerStatus{PeerURL: srv.URL, NodeID: "peer-1", Height: 6, Healthy: true}
	m.mu.Unlock()

	prev := m.BuildLinearPullPreview(context.Background(), 3, "h3", 8)
	if !prev.PlanReady || prev.PlannedCnt != 3 || prev.PlannedTo != 6 {
		t.Fatalf("unexpected preview: %+v", prev)
	}
}

func TestBuildLinearPullPreview_BootstrapGenesis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chain" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("from_height") != "0" {
			http.Error(w, "expected from_height=0", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blocks":[
			{"index":0,"hash":"g0","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000"}
		]}`))
	}))
	defer srv.Close()

	m := NewManager("n1", []string{srv.URL}, "", false, "", "ph1")
	m.mu.Lock()
	m.peers[srv.URL] = PeerStatus{
		PeerURL: srv.URL,
		NodeID:  "peer-1",
		Height:  0,
		Tip:     "g0",
		Healthy: true,
	}
	m.mu.Unlock()
	prev := m.BuildLinearPullPreview(context.Background(), 0, "", 8)
	if !prev.PlanReady || prev.PlannedCnt != 1 || prev.PlannedFrom != 0 || prev.PlannedTo != 0 {
		t.Fatalf("unexpected bootstrap preview: %+v", prev)
	}
	if len(prev.Hashes) != 1 || prev.Hashes[0] != "g0" {
		t.Fatalf("unexpected bootstrap hashes: %+v", prev.Hashes)
	}
}

func TestFetchBlocksByHashes(t *testing.T) {
	b1 := block.NewPoHBlock(1, "0", "m", 1, 20, 1, "", "formula")
	b2 := block.NewPoHBlock(2, b1.Hash, "m", 2, 27, 1, "", "formula")
	r1, _ := json.Marshal(b1)
	r2, _ := json.Marshal(b2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blocks":[` + string(r1) + `,` + string(r2) + `]}`))
	}))
	defer srv.Close()
	m := NewManager("n1", []string{srv.URL}, "", false, "", "ph1")
	got, err := m.FetchBlocksByHashes(context.Background(), srv.URL, []string{
		b1.Hash,
		b2.Hash,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got))
	}
}
