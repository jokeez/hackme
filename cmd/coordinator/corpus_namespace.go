package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
)

func addCorpusNamespaceRoute(mux *http.ServeMux, adminToken string, allowInsecure bool, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	mux.HandleFunc("/api/fuzz/pool/corpus/namespace", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			Namespace string `json:"namespace"`
			Seeds     []struct {
				InputU64   uint64 `json:"input_u64"`
				InputBytes string `json:"input_bytes"`
				Energy     int    `json:"energy"`
				Edge       int    `json:"edge"`
				Path       int    `json:"path"`
			} `json:"seeds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ns := strings.TrimSpace(req.Namespace)
		if ns == "" {
			http.Error(w, "namespace required", http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		seeds := make([]fuzzengine.PoolCorpusSeed, 0, len(req.Seeds))
		for _, s := range req.Seeds {
			var b []byte
			if strings.TrimSpace(s.InputBytes) != "" {
				var err error
				b, err = base64.StdEncoding.DecodeString(strings.TrimSpace(s.InputBytes))
				if err != nil {
					http.Error(w, "invalid input_bytes", http.StatusBadRequest)
					return
				}
			}
			seeds = append(seeds, fuzzengine.PoolCorpusSeed{
				Input: s.InputU64, InputBytes: b, Energy: s.Energy, Edge: s.Edge, Path: s.Path,
			})
		}
		if err := pf.UpsertNamespaceCorpusSeeds(r.Context(), ns, seeds, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "namespace": ns, "count": len(seeds)})
	})
}
