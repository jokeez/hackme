package workerfuzzloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseLeasePostsBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/work/release" || r.Method != http.MethodPost {
			t.Fatalf("path=%s method=%s", r.URL.Path, r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()
	err := ReleaseLease(context.Background(), srv.Client(), srv.URL, "tok", "w1", "camp-1", 42, "pub", "HMC-aaaaaaaaaaaaaaaa", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["worker_id"] != "w1" || got["campaign_id"] != "camp-1" {
		t.Fatalf("body=%v", got)
	}
	if int64(got["item_id"].(float64)) != 42 {
		t.Fatalf("item_id=%v", got["item_id"])
	}
	if got["miner_pubkey"] != "pub" {
		t.Fatalf("pubkey=%v", got["miner_pubkey"])
	}
}

func TestClaimCapsOmitHarnessByDefault(t *testing.T) {
	t.Setenv("HACKME_HUNT_HARNESS_EXEC", "")
	t.Setenv("HACKME_WORKER_VERSION", "")
	caps := claimCaps(Config{})
	if caps.HuntHarnessExec != "" {
		t.Fatalf("expected empty hunt harness, got %q", caps.HuntHarnessExec)
	}
}

func TestClaimCapsIncludeHarness(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "no_fuzz_work"})
	}))
	defer srv.Close()
	_, err := Claim(context.Background(), srv.Client(), srv.URL, "tok", "w1", "", "", ClaimCaps{
		WorkerVersion: "0.1.0-rc17.3", HuntHarnessExec: "libfuzzer_oneshot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["worker_version"] != "0.1.0-rc17.3" {
		t.Fatalf("version=%v", got["worker_version"])
	}
	if got["hunt_harness_exec"] != "libfuzzer_oneshot" {
		t.Fatalf("harness=%v", got["hunt_harness_exec"])
	}
}
