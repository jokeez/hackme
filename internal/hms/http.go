package hms

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
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
			ChunkID           string `json:"chunk_id"`
			WorkerID          string `json:"worker_id"`
			CiphertextSHA256  string `json:"ciphertext_sha256"`
			Size              uint64 `json:"size"`
			ErasureMetaHex    string `json:"erasure_meta_hex"`
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
	if os.Getenv("HMS_STRATUM_ENABLE") == "1" {
		go RunStratumBridge(coord, os.Getenv("HMS_STRATUM_ADDR"))
	}
}

func sha256Bytes(b []byte) [32]byte {
	return sha256.Sum256(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
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
	ip := clientIP(r)
	return ip == "127.0.0.1" || ip == "::1" || ip == ""
}
