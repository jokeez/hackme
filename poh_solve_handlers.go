package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type pohSolveOrderRequest struct {
	MinerAddress string `json:"miner_address"`
	FoundNonce   uint64 `json:"found_nonce"`
	TargetMod    uint64 `json:"target_mod"`
	OrderTaskID  string `json:"order_task_id"`
}

func (a *app) handlePohSolveOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req pohSolveOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	b, err := a.chain.SubmitOrderPoHSolve(r.Context(),
		strings.TrimSpace(req.MinerAddress),
		req.FoundNonce,
		req.TargetMod,
		strings.TrimSpace(req.OrderTaskID),
	)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"block_hash":  b.Hash,
		"block_index": b.Index,
		"miner":       b.MinerAddress,
		"order_task_id": strings.TrimSpace(req.OrderTaskID),
	})
}
