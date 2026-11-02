package workerfuzzloop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReleaseExtraHuntBatchClaimsKeepsOne(t *testing.T) {
	var released atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/work/release" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		released.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{HTTPClient: srv.Client(), Token: "t", WorkerID: "w1", LogPrefix: "test"}
	claims := []ClaimResp{
		{OK: true, CampaignID: "h1", ItemID: 1, TaskClass: "hunt", WorkKind: "hunt_shard"},
		{OK: true, CampaignID: "h1", ItemID: 2, TaskClass: "hunt", WorkKind: "hunt_shard"},
		{OK: true, CampaignID: "h1", ItemID: 3, TaskClass: "hunt", WorkKind: "hunt_shard"},
	}
	out := releaseExtraHuntBatchClaims(context.Background(), cfg, srv.URL, claims)
	if len(out) != 1 || out[0].ItemID != 1 {
		t.Fatalf("out=%#v", out)
	}
	if released.Load() != 2 {
		t.Fatalf("released=%d want 2", released.Load())
	}
}

func TestReleaseExtraHuntBatchClaimsIgnoresDig(t *testing.T) {
	cfg := Config{HTTPClient: http.DefaultClient, WorkerID: "w1"}
	claims := []ClaimResp{
		{OK: true, CampaignID: "d1", ItemID: 1, TaskClass: "dig"},
		{OK: true, CampaignID: "d1", ItemID: 2, TaskClass: "dig"},
	}
	out := releaseExtraHuntBatchClaims(context.Background(), cfg, "http://127.0.0.1:1", claims)
	if len(out) != 2 {
		t.Fatalf("dig batch must stay intact, got %d", len(out))
	}
}
