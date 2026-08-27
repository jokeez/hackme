package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeReleaseVersion(t *testing.T) {
	cases := map[string]string{
		"0.1.0-rc15":        "0.1.0-rc15",
		"HackMe 0.1.0-rc15": "0.1.0-rc15",
		"version=0.1.0":     "0.1.0",
		"  1.2.3  ":         "1.2.3",
	}
	for in, want := range cases {
		if got := normalizeReleaseVersion(in); got != want {
			t.Fatalf("normalizeReleaseVersion(%q)=%q want %q", in, got, want)
		}
	}
}

func TestUpdateAvailable(t *testing.T) {
	if updateAvailable("0.1.0-rc15", "0.1.0-rc15") {
		t.Fatal("same version must not need update")
	}
	if !updateAvailable("0.1.0-rc14", "0.1.0-rc15") {
		t.Fatal("different versions must need update")
	}
	if updateAvailable("", "0.1.0-rc15") {
		t.Fatal("empty local must not claim update")
	}
}

func TestHandleUpdatesCheck(t *testing.T) {
	dir := t.TempDir()
	latestPath := filepath.Join(dir, "latest.json")
	doc := map[string]any{
		"schema":  latestSchemaV1,
		"app":     "HackMe",
		"version": "9.9.9-test",
		"commit":  "deadbeef",
		"channel": "stable",
		"notes":   "test",
		"platforms": []map[string]any{
			{"id": "linux", "file": "hackme_9.9.9-test_linux.tar.gz", "sha256": "abc", "kind": "tar.gz", "url": "https://example/x"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(latestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, latestPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("HACKME_LATEST_URL", srv.URL+"/latest.json")
	updateCheckCached.mu.Lock()
	updateCheckCached.payload = nil
	updateCheckCached.at = time.Time{}
	updateCheckCached.mu.Unlock()

	a := &app{}
	req := httptest.NewRequest(http.MethodGet, "/api/updates/check?force=1", nil)
	rr := httptest.NewRecorder()
	a.handleUpdatesCheck(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("ok=%v err=%v", out["ok"], out["error"])
	}
	if out["remote_version"] != "9.9.9-test" {
		t.Fatalf("remote_version=%v", out["remote_version"])
	}
	if out["update_available"] != true {
		t.Fatalf("expected update_available true, got %v (local=%v)", out["update_available"], out["local_version"])
	}
}
