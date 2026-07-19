package hms

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// RegisterHTTP mounts HMS lane routes on mux.
func RegisterHTTP(mux *http.ServeMux, coord *Coordinator, adminToken, workerToken string) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":"hms-coordinator"}`))
	})
	mux.HandleFunc("/api/pool/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, coord.PoolStats())
	})

	auth := func(r *http.Request, needWorker bool) bool {
		if adminToken != "" && bearerOK(r, adminToken) {
			return true
		}
		if needWorker && workerToken != "" && bearerOK(r, workerToken) {
			return true
		}
		if workerToken == "" && adminToken == "" && loopbackOnly(r) {
			return true
		}
		return false
	}

	mux.HandleFunc("/api/storage/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !coord.Guard().AllowHTTP(r, "") {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		var req struct {
			WorkerID  string `json:"worker_id"`
			PubkeyHex string `json:"pubkey_hex"`
			QuotaGB   int    `json:"quota_gb"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := coord.RegisterStorageWorker(strings.TrimSpace(req.WorkerID), strings.TrimSpace(req.PubkeyHex), req.QuotaGB); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/storage/challenge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			WorkerID string `json:"worker_id"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		wid := strings.TrimSpace(req.WorkerID)
		if !coord.Guard().AllowHTTP(r, wid) {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		out, err := coord.IssueChallenge(wid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("/api/storage/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			StorageSubmitPayload
			PubkeyHex string `json:"pubkey_hex"`
			SigHex    string `json:"sig_hex"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !coord.Guard().AllowHTTP(r, req.WorkerID) {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		proof, _ := hex.DecodeString(strings.TrimSpace(req.ProofHex))
		if err := coord.SubmitStorageProof(req.StorageSubmitPayload, req.PubkeyHex, req.SigHex, proof); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/storage/chunk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, false) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ChunkID          string `json:"chunk_id"`
			WorkerID         string `json:"worker_id"`
			CiphertextSHA256 string `json:"ciphertext_sha256"`
			Size             uint64 `json:"size"`
			ErasureMetaHex   string `json:"erasure_meta_hex"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		ct, err := hex.DecodeString(strings.TrimSpace(req.CiphertextSHA256))
		if err != nil || len(ct) == 0 {
			http.Error(w, "ciphertext_sha256 required", http.StatusBadRequest)
			return
		}
		if len(ct) != 32 {
			sum := sha256Bytes(ct)
			ct = sum[:]
		}
		meta, _ := hex.DecodeString(strings.TrimSpace(req.ErasureMetaHex))
		if err := coord.AssignChunk(req.ChunkID, req.WorkerID, ct, req.Size, meta); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/seal/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			WorkerID  string `json:"worker_id"`
			PubkeyHex string `json:"pubkey_hex"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := coord.RegisterSealWorker(strings.TrimSpace(req.WorkerID), strings.TrimSpace(req.PubkeyHex)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/seal/work", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		out, err := coord.SealWork()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("/api/seal/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !auth(r, true) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			SealSubmitPayload
			PubkeyHex string `json:"pubkey_hex"`
			SigHex    string `json:"sig_hex"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !coord.Guard().AllowHTTP(r, req.WorkerID) {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		if err := coord.SubmitSeal(req.SealSubmitPayload, req.PubkeyHex, req.SigHex); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "accepted": true})
	})

	// Stratum bridge for ASIC (basic); port via HMS_STRATUM_ADDR env in cmd.
	registerMarketRoutes(mux, coord)
	if os.Getenv("HMS_STRATUM_ENABLE") == "1" {
		go RunStratumBridge(coord, os.Getenv("HMS_STRATUM_ADDR"))
	}
}

func registerMarketRoutes(mux *http.ServeMux, coord *Coordinator) {
	mux.HandleFunc("/api/market/capacity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap, err := coord.NetworkCapacity()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "capacity": snap})
	})
	mux.HandleFunc("/api/market/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "market": coord.MarketStats()})
	})
	mux.HandleFunc("/api/market/pricing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "pricing": MarketPricingPolicySnapshot()})
	})
	mux.HandleFunc("/api/market/quote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SizeBytes     int64 `json:"size_bytes"`
			RetentionDays int   `json:"retention_days"`
		}
		if !readJSON(r, &req) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		q, err := QuoteStorageOrder(req.SizeBytes, req.RetentionDays)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		capSnap, capErr := coord.EnsureCapacity(RequiredCapacityBytes(req.SizeBytes))
		if capErr != nil {
			writeJSONStatus(w, marketHTTPStatus(capErr), map[string]any{
				"status": "error", "error": capErr.Error(), "capacity": capSnap,
			})
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "quote": q, "capacity": capSnap})
	})
	mux.HandleFunc("/api/market/orders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list, err := coord.ListStorageOrders(50)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"status": "ok", "orders": list})
		case http.MethodPost:
			var req struct {
				Label         string `json:"label"`
				ClientRef     string `json:"client_ref"`
				SizePlanBytes int64  `json:"size_plan_bytes"`
				RetentionDays int    `json:"retention_days"`
				QuoteHash     string `json:"quote_hash"`
				PaymentID     string `json:"payment_id"`
				PaymentProof  string `json:"payment_proof"`
			}
			if !readJSON(r, &req) {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			// Pilot skip only on real TCP loopback — never trust X-Forwarded-For.
			out, err := coord.CreateStorageOrder(req.Label, req.ClientRef, req.SizePlanBytes, req.RetentionDays, req.QuoteHash, req.PaymentID, req.PaymentProof, loopbackOnly(r))
			if err != nil {
				http.Error(w, err.Error(), marketHTTPStatus(err))
				return
			}
			writeJSON(w, map[string]any{"status": "ok", "result": out})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/seal/payouts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		epochID := int64(atoiDefault(r.URL.Query().Get("epoch_id"), 0))
		if epochID <= 0 {
			http.Error(w, "epoch_id required", http.StatusBadRequest)
			return
		}
		out, err := coord.EpochSealSettlement(epochID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "settlement": out})
	})
	mux.HandleFunc("/api/market/orders/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/market/orders/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		orderID := parts[0]
		if len(parts) == 2 && parts[1] == "upload" && r.Method == http.MethodPost {
			token := strings.TrimSpace(r.Header.Get("X-HMS-Upload-Token"))
			var req struct {
				ChunkIndex    int    `json:"chunk_index"`
				CiphertextHex string `json:"ciphertext_hex"`
			}
			if !readJSON(r, &req) {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			ciphertext, err := hex.DecodeString(strings.TrimSpace(req.CiphertextHex))
			if err != nil || len(ciphertext) == 0 {
				http.Error(w, "ciphertext required", http.StatusBadRequest)
				return
			}
			if !coord.Guard().AllowHTTP(r, orderID) {
				http.Error(w, "rate limit", http.StatusTooManyRequests)
				return
			}
			out, err := coord.UploadOrderChunk(orderID, token, req.ChunkIndex, ciphertext)
			if err != nil {
				http.Error(w, err.Error(), marketHTTPStatus(err))
				return
			}
			writeJSON(w, out)
			return
		}
		if len(parts) == 2 && parts[1] == "health" && r.Method == http.MethodGet {
			out, err := coord.OrderHealth(orderID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]any{"status": "ok", "health": out})
			return
		}
		if len(parts) == 2 && parts[1] == "chunks" && r.Method == http.MethodGet {
			list, err := coord.ListOrderChunks(orderID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"status": "ok", "chunks": list})
			return
		}
		if len(parts) == 3 && parts[1] == "download" && r.Method == http.MethodGet {
			token := strings.TrimSpace(r.Header.Get("X-HMS-Upload-Token"))
			if token == "" {
				token = strings.TrimSpace(r.URL.Query().Get("token"))
			}
			idx := atoiDefault(parts[2], -1)
			if idx < 0 {
				http.Error(w, "invalid chunk index", http.StatusBadRequest)
				return
			}
			data, chunkID, err := coord.DownloadOrderChunk(orderID, token, idx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-HMS-Chunk-Id", chunkID)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}
		if len(parts) == 2 && parts[1] == "complete" && r.Method == http.MethodPost {
			token := strings.TrimSpace(r.Header.Get("X-HMS-Upload-Token"))
			if err := coord.verifyUploadToken(orderID, token); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			var chunkCount int
			_ = coord.db.QueryRow(`SELECT COUNT(*) FROM hms_order_chunks WHERE order_id=?`, orderID).Scan(&chunkCount)
			if chunkCount < 1 {
				http.Error(w, "no chunks uploaded", http.StatusBadRequest)
				return
			}
			now := time.Now().Unix()
			_, _ = coord.db.Exec(`UPDATE hms_orders SET status=?, updated_unix=? WHERE order_id=?`, OrderStatusStored, now, orderID)
			_ = coord.refreshOrderHealth()
			o, _ := coord.GetStorageOrder(orderID)
			status := OrderStatusStored
			healthStatus := HealthOK
			healthDetail := ""
			if o != nil {
				status = o.Status
				healthStatus = o.HealthStatus
				healthDetail = o.HealthDetail
			}
			writeJSON(w, map[string]any{
				"ok": true, "order_id": orderID, "status": status,
				"health_status": healthStatus, "health_detail": healthDetail,
			})
			return
		}
		if len(parts) == 1 && r.Method == http.MethodGet {
			o, err := coord.GetStorageOrder(orderID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]any{"status": "ok", "order": o})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
}

func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func sha256Bytes(b []byte) [32]byte {
	return sha256.Sum256(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return false
	}
	return json.Unmarshal(body, v) == nil
}

func bearerOK(r *http.Request, token string) bool {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		h = strings.TrimSpace(h[7:])
	}
	if h == "" {
		h = strings.TrimSpace(r.Header.Get("X-Hackme-Admin-Token"))
	}
	return subtle.ConstantTimeCompare([]byte(h), []byte(token)) == 1
}

func loopbackOnly(r *http.Request) bool {
	// Auth must use RemoteAddr — never trust X-Forwarded-For for loopback skips.
	host := ""
	if r != nil {
		host = r.RemoteAddr
		if i := strings.LastIndex(host, ":"); i > 0 {
			host = host[:i]
		}
		host = strings.Trim(host, "[]")
	}
	return host == "127.0.0.1" || host == "::1" || host == ""
}
