package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func TestCorpusNamespaceUploadRoundtrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "corpus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pf := &poolfuzz.Service{DB: db}
	mux := http.NewServeMux()
	addCorpusNamespaceRoute(mux, "admin-tok", false, pf)

	body, _ := json.Marshal(map[string]any{
		"namespace": "test-ns",
		"seeds": []map[string]any{
			{"input_u64": 7, "input_bytes": "AQI=", "energy": 4, "edge": 2, "path": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/corpus/namespace", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	svc := &poolfuzz.Service{DB: db}
	seeds, err := svc.ListNamespaceCorpus(context.Background(), "test-ns", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || seeds[0].Input != 7 || seeds[0].Energy != 4 {
		t.Fatalf("seeds=%+v", seeds)
	}
	if string(seeds[0].InputBytes) != "\x01\x02" {
		t.Fatalf("input_bytes=%q", seeds[0].InputBytes)
	}
}
