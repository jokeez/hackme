package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type integratorRegisterRequest struct {
	Label string `json:"label"`
	Email string `json:"email"`
}

func (a *app) handleIntegratorAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/integrator")
	path = strings.Trim(path, "/")
	if path == "" {
		if r.Method == http.MethodGet {
			a.handleIntegratorStatus(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	switch path {
	case "register":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleIntegratorRegister(w, r)
	case "rotate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleIntegratorRotate(w, r)
	case "status":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleIntegratorStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *app) handleIntegratorRegister(w http.ResponseWriter, r *http.Request) {
	if !integratorSelfRegisterEnabled() {
		writeAPIError(w, http.StatusForbidden, "self_register_disabled", "integrator self-registration is disabled on this node", nil)
		return
	}
	if integratorStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "integrator_unavailable", "integrator store not ready", nil)
		return
	}
	if !a.allowRate("integrator_register:"+clientIP(r), 3) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many registration attempts; try again later", nil)
		return
	}
	var req integratorRegisterRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = strings.TrimSpace(req.Email)
	}
	id, token, err := integratorStore.Register(label, clientIP(r), integratorMaxActive())
	if err != nil {
		code := "register_failed"
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "max active") {
			code = "max_tokens"
			status = http.StatusTooManyRequests
		}
		writeAPIError(w, status, code, err.Error(), nil)
		return
	}
	logAdminAction(r, "integrator_register:"+id)
	writeJSON(w, map[string]any{
		"ok":              true,
		"integrator_id":   id,
		"developer_token": token,
		"header":          "X-Hackme-Developer-Token",
		"warning":         "Save this token now — it cannot be retrieved again. Use hackme-fuzzing rotate to replace it.",
		"cli": map[string]string{
			"save":   "hackme-fuzzing register --save",
			"rotate": "hackme-fuzzing rotate",
		},
	})
}

func (a *app) handleIntegratorRotate(w http.ResponseWriter, r *http.Request) {
	if integratorStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "integrator_unavailable", "integrator store not ready", nil)
		return
	}
	if !a.allowRate("integrator_rotate:"+clientIP(r), 10) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	old := extractDeveloperSecret(r)
	if old == "" {
		writeAPIError(w, http.StatusUnauthorized, "token_required", "send current X-Hackme-Developer-Token", nil)
		return
	}
	if !integratorTokenValid(old) {
		writeAPIError(w, http.StatusUnauthorized, "invalid_token", "invalid or revoked token", nil)
		return
	}
	id, newTok, err := integratorStore.Rotate(old)
	if err != nil {
		// Legacy env-only token cannot rotate in store — instruct re-register or operator
		if developerTokenFromEnv() != "" && secretsEqualConstantTime(old, developerTokenFromEnv()) {
			writeAPIError(w, http.StatusBadRequest, "legacy_token", "legacy HACKME_DEVELOPER_TOKEN cannot rotate via API; use POST /api/integrator/register for a managed token", nil)
			return
		}
		writeAPIError(w, http.StatusUnauthorized, "rotate_failed", err.Error(), nil)
		return
	}
	logAdminAction(r, "integrator_rotate:"+id)
	writeJSON(w, map[string]any{
		"ok":              true,
		"integrator_id":   id,
		"developer_token": newTok,
		"warning":         "Old token is invalid immediately. Save the new token.",
	})
}

func (a *app) handleIntegratorStatus(w http.ResponseWriter, r *http.Request) {
	active := 0
	if integratorStore != nil {
		active = integratorStore.ActiveCount()
	}
	writeJSON(w, map[string]any{
		"ok":                    true,
		"self_register_enabled": integratorSelfRegisterEnabled(),
		"active_tokens":         active,
		"max_tokens":            integratorMaxActive(),
		"register_path":         "/api/integrator/register",
		"rotate_path":           "/api/integrator/rotate",
	})
}
