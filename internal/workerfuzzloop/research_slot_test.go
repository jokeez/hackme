package workerfuzzloop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzengine"
)

func TestResearchSlotDefaultOn(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "")
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")
	cfg := ResearchSlotFromEnv()
	if !cfg.Enabled {
		t.Fatal("research slot must default ON for hybrid Hunt (empty env)")
	}
}

func TestResearchSlotExplicitOff(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "0")
	cfg := ResearchSlotFromEnv()
	if cfg.Enabled {
		t.Fatal("explicit 0 must disable research slot")
	}
	res := MaybeRunResearchSlot(context.Background(), ResearchSlotRun{Config: cfg})
	if res.Ran || res.SkippedReason != "disabled" {
		t.Fatalf("res=%+v", res)
	}
}

func TestResearchSlotHuntOnlyWithoutDigFlag(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT_DIG", "")
	cfg := ResearchSlotFromEnv()
	if !cfg.Enabled || cfg.AllowDig {
		t.Fatalf("cfg=%+v", cfg)
	}
	res := MaybeRunResearchSlot(context.Background(), ResearchSlotRun{
		Config:   cfg,
		Claim:    ClaimResp{CampaignID: "c", ItemID: 1, TaskClass: "dig"},
		CoordURL: "http://127.0.0.1:9", WorkerID: "w",
	})
	if res.Ran || res.SkippedReason != "hunt_only" {
		t.Fatalf("dig without AllowDig must skip, got %+v", res)
	}
}

func TestPoolSeedFromResearchUntouchedByResearchSlot(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")
	if Truthy(os.Getenv("HACKME_POOL_SEED_FROM_RESEARCH")) {
		t.Fatal("SEED_FROM_RESEARCH must stay default OFF")
	}
}

func TestCollectResearchDeltaNewUnitsAndCrashes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.bin"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotCorpusHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.bin"), []byte("brand-new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crash-1"), []byte("boom"), 0o600); err != nil {
		t.Fatal(err)
	}
	seeds, crashes, err := collectResearchDelta(dir, before, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || string(seeds[0].InputBytes) != "brand-new" {
		t.Fatalf("seeds=%v", seeds)
	}
	if len(crashes) != 1 || string(crashes[0]) != "boom" {
		t.Fatalf("crashes=%v", crashes)
	}
}

func TestCollectResearchDeltaFindsHuntRunCrashes(t *testing.T) {
	dir := t.TempDir()
	crashDir := filepath.Join(dir, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crashDir, "crash-lf"), []byte("lf-boom"), 0o600); err != nil {
		t.Fatal(err)
	}
	seeds, crashes, err := collectResearchDelta(dir, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 0 {
		t.Fatalf("seeds=%v", seeds)
	}
	if len(crashes) != 1 || string(crashes[0]) != "lf-boom" {
		t.Fatalf("crashes=%v", crashes)
	}
}

func TestMaybeRunResearchSlotUploadsDelta(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	cfg := ResearchSlotFromEnv()
	dir := t.TempDir()
	corpus := filepath.Join(dir, "reports", "oss-cve-libfuzzer", "jsmn", "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := researchLFRunner
	t.Cleanup(func() { researchLFRunner = prev })
	researchLFRunner = func(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
		if err := os.WriteFile(filepath.Join(corpus, "unit-a"), []byte("delta-a"), 0o600); err != nil {
			return 0, err
		}
		if err := os.WriteFile(filepath.Join(corpus, "crash-x"), []byte("crash-bytes"), 0o600); err != nil {
			return 0, err
		}
		return 2, nil
	}

	var gotNS string
	var gotSeeds int
	var gotCrashes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/work/corpus_delta" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Namespace string   `json:"namespace"`
			Seeds     []any    `json:"seeds"`
			Crashes   []string `json:"crashes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotNS = body.Namespace
		gotSeeds = len(body.Seeds)
		gotCrashes = len(body.Crashes)
		if len(body.Crashes) > 0 {
			b, _ := base64.StdEncoding.DecodeString(body.Crashes[0])
			if string(b) != "crash-bytes" {
				http.Error(w, "bad crash", http.StatusBadRequest)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "seeds_accepted": gotSeeds, "findings": 0,
		})
	}))
	t.Cleanup(srv.Close)

	res := MaybeRunResearchSlot(context.Background(), ResearchSlotRun{
		Config: cfg, CoordURL: srv.URL, Token: "tok", WorkerID: "w1",
		HTTPClient: srv.Client(), RepoRoot: dir,
		Claim: ClaimResp{
			CampaignID: "camp", ItemID: 7, UpstreamTargetID: "jsmn",
			WorkKind: "hunt_shard", TaskClass: "hunt",
		},
	})
	if !res.Ran || res.CorpusDeltas != 1 || res.Crashes != 1 {
		t.Fatalf("res=%+v", res)
	}
	if gotNS != "research:jsmn" || gotSeeds != 1 || gotCrashes != 1 {
		t.Fatalf("upload ns=%q seeds=%d crashes=%d", gotNS, gotSeeds, gotCrashes)
	}
}

func TestUploadCorpusDeltaRetriesTransient(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if n == 1 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "seeds_accepted": 1, "findings": 0})
	}))
	t.Cleanup(srv.Close)
	acc, findings, err := uploadResearchCorpusDelta(context.Background(), srv.Client(), srv.URL, "tok", "w", "m", "c", 1, "research:jsmn",
		[]fuzzengine.PoolCorpusSeed{{InputBytes: []byte("unit"), Energy: 2}}, nil, "libfuzzer", "t")
	if err != nil {
		t.Fatal(err)
	}
	if acc != 1 || findings != 0 || n != 2 {
		t.Fatalf("acc=%d findings=%d attempts=%d", acc, findings, n)
	}
}

func TestUploadCorpusDeltaRetriesOneDroppedConnection(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_ = conn.Close()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "seeds_accepted": 1, "findings": 0})
	}))
	t.Cleanup(srv.Close)
	acc, findings, err := uploadResearchCorpusDelta(context.Background(), srv.Client(), srv.URL, "tok", "w", "m", "c", 1, "research:jsmn",
		[]fuzzengine.PoolCorpusSeed{{InputBytes: []byte("unit"), Energy: 2}}, nil, "libfuzzer", "t")
	if err != nil {
		t.Fatal(err)
	}
	if acc != 1 || findings != 0 || n != 2 {
		t.Fatalf("dropped connection must retry once: acc=%d findings=%d attempts=%d", acc, findings, n)
	}
}

func TestUploadCorpusDeltaDoesNotRetry403(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	_, _, err := uploadResearchCorpusDelta(context.Background(), srv.Client(), srv.URL, "tok", "w", "m", "c", 1, "research:jsmn",
		[]fuzzengine.PoolCorpusSeed{{InputBytes: []byte("unit")}}, nil, "", "")
	if err == nil || n != 1 {
		t.Fatalf("403 must not retry: err=%v attempts=%d", err, n)
	}
}

func TestUploadCorpusDeltaDoesNotRetryAuth(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	_, _, err := uploadResearchCorpusDelta(context.Background(), srv.Client(), srv.URL, "tok", "w", "m", "c", 1, "research:jsmn",
		[]fuzzengine.PoolCorpusSeed{{InputBytes: []byte("unit")}}, nil, "", "")
	if err == nil || n != 1 {
		t.Fatalf("err=%v attempts=%d", err, n)
	}
}

func TestFetchCorpusSnapshotRetriesTransient(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "corpus_snapshot_sha256": "abc",
			"corpus_seeds": []map[string]any{{"input_bytes_hex": "61"}},
		})
	}))
	t.Cleanup(srv.Close)
	seeds, sha, err := FetchCorpusSnapshot(context.Background(), srv.Client(), srv.URL, "tok", "w", "c", 3)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc" || len(seeds) != 1 || n != 2 {
		t.Fatalf("sha=%q seeds=%d attempts=%d", sha, len(seeds), n)
	}
}

func TestMaybeRunResearchSlotNoTarget(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	cfg := ResearchSlotFromEnv()
	res := MaybeRunResearchSlot(context.Background(), ResearchSlotRun{
		Config: cfg, CoordURL: "http://x", WorkerID: "w",
		Claim: ClaimResp{CampaignID: "c", ItemID: 1, WorkKind: "hunt_shard"},
	})
	if res.SkippedReason != "no_target" {
		t.Fatalf("res=%+v", res)
	}
}
