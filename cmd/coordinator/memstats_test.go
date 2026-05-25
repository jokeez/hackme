package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testWorkManagerBare() *workManager {
	return &workManager{
		defaultBatch:           1000,
		targetMod:              1_000_000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		abuse:                  make(map[string]workerAbuseState),
		ipAbuse:                make(map[string]workerAbuseState),
		acceptedSignedPayloads: make(map[string]struct{}),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
	}
}

func TestRuntimeMemSnapshotFields(t *testing.T) {
	wm := testWorkManagerBare()
	snap := wm.runtimeMemSnapshot()
	if _, ok := snap["heap_alloc_mb"]; !ok {
		t.Fatal("missing heap_alloc_mb")
	}
	if _, ok := snap["ip_abuse_entries"]; !ok {
		t.Fatal("missing ip_abuse_entries")
	}
}

func TestAdminMemstatsAndGC(t *testing.T) {
	wm := testWorkManagerBare()
	mux := http.NewServeMux()
	addWorkRoutes(mux, "test-admin-token", "", false, nil, wm, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/work/admin/memstats", nil)
	req.Header.Set("X-Hackme-Admin-Token", "test-admin-token")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("memstats status=%d body=%s", rr.Code, rr.Body.String())
	}
	var snap map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap["ok"] != true {
		t.Fatalf("expected ok=true: %v", snap)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/work/admin/gc", nil)
	req2.Header.Set("X-Hackme-Admin-Token", "test-admin-token")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("gc status=%d body=%s", rr2.Code, rr2.Body.String())
	}
}
