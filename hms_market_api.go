package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/hms"
)

func (a *app) hmsCoordinatorURL() string {
	u := strings.TrimSpace(os.Getenv("HACKME_HMS_COORDINATOR_URL"))
	if u == "" {
		u = "http://127.0.0.1:18082"
	}
	return strings.TrimRight(u, "/")
}

func (a *app) proxyHMSCoordinator(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	url := a.hmsCoordinatorURL() + path
	if r != nil && r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range []string{"X-HMS-Upload-Token", "Authorization", "X-Hackme-Admin-Token"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (a *app) handleHMSMarketPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "pricing": hms.MarketPricingPolicySnapshot()})
}

func (a *app) handleHMSMarketQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var req struct {
		SizeBytes     int64 `json:"size_bytes"`
		RetentionDays int   `json:"retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	q, err := hms.QuoteStorageOrder(req.SizeBytes, req.RetentionDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "quote": q})
}

func (a *app) handleHMSMarketOrders(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.proxyHMSCoordinator(w, r, http.MethodGet, "/api/market/orders", nil)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Label          string `json:"label"`
			ClientRef      string `json:"client_ref"`
			SizePlanBytes  int64  `json:"size_plan_bytes"`
			RetentionDays  int    `json:"retention_days"`
			QuoteHash      string `json:"quote_hash"`
			PaymentID      string `json:"payment_id"`
			SkipPayment    bool   `json:"skip_payment"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		idem := strings.TrimSpace(req.IdempotencyKey)
		if idem == "" {
			idem = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
		paymentID := strings.TrimSpace(req.PaymentID)
		var paymentProof string
		if !req.SkipPayment && paymentID == "" {
			if strings.TrimSpace(req.QuoteHash) == "" {
				http.Error(w, "quote_hash required", http.StatusBadRequest)
				return
			}
			pay, err := a.chain.PayHMSStorageMarket(r.Context(), req.Label, req.SizePlanBytes, req.RetentionDays, req.QuoteHash, idem)
			if err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			paymentID = pay.PaymentID
			paymentProof = pay.PaymentProof
		}
		fwd, _ := json.Marshal(map[string]any{
			"label":           req.Label,
			"client_ref":      req.ClientRef,
			"size_plan_bytes": req.SizePlanBytes,
			"retention_days":  req.RetentionDays,
			"quote_hash":      req.QuoteHash,
			"payment_id":      paymentID,
			"payment_proof":   paymentProof,
		})
		a.proxyHMSCoordinator(w, r, http.MethodPost, "/api/market/orders", fwd)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleHMSMarketOrdersPath(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/hms/market/orders/")
	var body []byte
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20+1024)
		body, _ = io.ReadAll(r.Body)
	}
	a.proxyHMSCoordinator(w, r, r.Method, "/api/market/orders/"+path, body)
}

func (a *app) handleHMSPoolStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.proxyHMSCoordinator(w, r, http.MethodGet, "/api/pool/stats", nil)
}

func (a *app) handleHMSMarketStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.proxyHMSCoordinator(w, r, http.MethodGet, "/api/market/stats", nil)
}

func (a *app) handleHMSMarketCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.proxyHMSCoordinator(w, r, http.MethodGet, "/api/market/capacity", nil)
}

func registerHMSMarketRoutes(mux *http.ServeMux, a *app) {
	mux.HandleFunc("/api/hms/pool/stats", a.handleHMSPoolStats)
	mux.HandleFunc("/api/hms/market/stats", a.handleHMSMarketStats)
	mux.HandleFunc("/api/hms/market/capacity", a.handleHMSMarketCapacity)
	mux.HandleFunc("/api/hms/market/pricing", a.handleHMSMarketPricing)
	mux.HandleFunc("/api/hms/market/quote", a.handleHMSMarketQuote)
	mux.HandleFunc("/api/hms/market/orders", a.handleHMSMarketOrders)
	mux.HandleFunc("/api/hms/market/orders/", a.handleHMSMarketOrdersPath)
}
