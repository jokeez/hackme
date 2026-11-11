package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/poolsync"
)

func TestHuntHarnessPublishPreflightTooLarge(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-admin-test")
	root := a.repoRoot()
	t.Setenv("HACKME_REPO_ROOT", root)

	dir := filepath.Join(root, ".cache", "hunt-harness-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := filepath.Join(dir, "deadbeefdeadbeef.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(poolsync.CoordinatorHarnessUploadMaxBytes) + 1
	if _, err := f.WriteAt([]byte{1}, limit-1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	body, _ := json.Marshal(map[string]any{
		"harness_hash": "deadbeefdeadbeef",
		"binary_path":  p,
		"source_rel":   "test/harness.c",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/harness/publish", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "hunt-admin-test")
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != poolsync.CodeHarnessTooLarge {
		t.Fatalf("code=%v body=%s", resp["code"], rec.Body.String())
	}
}

func TestHuntCampaignOneShotRequiresBinaryPath(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-admin-test")
	body, _ := json.Marshal(map[string]any{
		"package": "hunt_lite",
		"title":   "oneshot-missing-bin",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/campaigns/one-shot", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "hunt-admin-test")
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "binary_path") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestWriteHarnessSyncAPIErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{fmt.Errorf("pool harness sync HTTP 413: nginx"), poolsync.CodeHarnessTooLarge, http.StatusRequestEntityTooLarge},
		{fmt.Errorf("pool harness sync HTTP 429: orders publish rate limit exceeded"), poolsync.CodePublishRateLimited, http.StatusTooManyRequests},
		{fmt.Errorf("pool harness sync HTTP 502: bad gateway"), poolsync.CodeHarnessSyncFailed, http.StatusBadGateway},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeHarnessSyncAPIError(rec, tc.err, map[string]any{"harness_hash": "abc"})
		if rec.Code != tc.status {
			t.Fatalf("status=%d want %d body=%s", rec.Code, tc.status, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp["code"] != tc.code {
			t.Fatalf("code=%v want %s", resp["code"], tc.code)
		}
		if resp["ok"] != false {
			t.Fatalf("ok=%v", resp["ok"])
		}
	}
}
