package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
)

// addCorpusDeltaRoute registers POST /api/fuzz/work/corpus_delta (worker-authed, lease-bound).
func addCorpusDeltaRoute(mux *http.ServeMux, adminToken, workerToken string, allowInsecure bool, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	mux.HandleFunc("/api/fuzz/work/corpus_delta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID   string `json:"worker_id"`
			CampaignID string `json:"campaign_id"`
			ItemID     int64  `json:"item_id"`
			Namespace  string `json:"namespace"`
			MinerAddr  string `json:"miner_address"`
			Seeds      []struct {
				InputU64   uint64 `json:"input_u64"`
				InputBytes string `json:"input_bytes"`
				Energy     int    `json:"energy"`
				Edge       int    `json:"edge"`
				Path       int    `json:"path"`
			} `json:"seeds"`
			Crashes []string `json:"crashes"` // base64 crash artifacts
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
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
		crashes := make([][]byte, 0, len(req.Crashes))
		for _, c := range req.Crashes {
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c))
			if err != nil {
				http.Error(w, "invalid crash bytes", http.StatusBadRequest)
				return
			}
			crashes = append(crashes, b)
		}
		out, err := pf.AcceptResearchCorpusDelta(r.Context(), poolfuzz.ResearchCorpusDeltaRequest{
			WorkerID:   workerID,
			CampaignID: strings.TrimSpace(req.CampaignID),
			ItemID:     req.ItemID,
			Namespace:  strings.TrimSpace(req.Namespace),
			MinerAddr:  strings.TrimSpace(req.MinerAddr),
			Seeds:      seeds,
			Crashes:    crashes,
		})
		if err != nil {
			msg := err.Error()
			code := http.StatusBadRequest
			if strings.Contains(msg, "authentication") {
				code = http.StatusUnauthorized
			} else if strings.Contains(msg, "active lease") {
				code = http.StatusForbidden
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": msg})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"namespace":         out.Namespace,
			"seeds_accepted":    out.SeedsAccepted,
			"crashes_replay_ok": out.CrashesReplayOK,
			"crashes_rejected":  out.CrashesRejected,
			"findings":          out.Findings,
		})
	})
}
