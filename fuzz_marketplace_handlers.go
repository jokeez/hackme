package main

import (
	"net/http"

	"hackme/internal/poolfuzz"
)

func (a *app) handleFuzzMarketplace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	svc := &poolfuzz.Service{DB: a.db}
	items, err := svc.ListPublicCampaigns(r.Context(), 50)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "marketplace_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "campaigns": items})
}
