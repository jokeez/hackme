package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hackme/internal/integrator"
)

func TestIntegratorRegisterRotateHTTP(t *testing.T) {
	dir := t.TempDir()
	st, err := integrator.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	integratorStore = st
	t.Setenv("HACKME_INTEGRATOR_SELF_REGISTER", "1")
	t.Setenv("HACKME_ADMIN_TOKEN", "")

	a := &app{rlHits: make(map[string]rateSlot), rlBan: make(map[string]int64)}

	regBody, _ := json.Marshal(map[string]string{"label": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/integrator/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleIntegratorRegister(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register %d %s", rec.Code, rec.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	tok, _ := reg["developer_token"].(string)
	if tok == "" {
		t.Fatal("no token")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req2.Header.Set("X-Hackme-Developer-Token", tok)
	rec2 := httptest.NewRecorder()
	if !requireDeveloperTasksAuth(rec2, req2) {
		t.Fatal("tasks auth with registered token")
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/integrator/rotate", nil)
	req3.Header.Set("X-Hackme-Developer-Token", tok)
	rec3 := httptest.NewRecorder()
	a.handleIntegratorRotate(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("rotate %d %s", rec3.Code, rec3.Body.String())
	}

	if integratorStore.Validate(tok) {
		t.Fatal("old token should be invalid")
	}
	if _, err := os.Stat(filepath.Join(dir, "integrator_tokens.json")); err != nil {
		t.Fatal(err)
	}
}
