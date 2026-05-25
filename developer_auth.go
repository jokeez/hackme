package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// developerTokenFromEnv is a scoped integrator secret (tasks read/create only).
func developerTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv("HACKME_DEVELOPER_TOKEN"))
}

func developerAuthEnabled() bool {
	return developerTokenFromEnv() != ""
}

func extractDeveloperSecret(r *http.Request) string {
	if s := strings.TrimSpace(r.Header.Get("X-Hackme-Developer-Token")); s != "" {
		return s
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// developerRequestAuthed is true for a valid developer token or full admin.
func developerRequestAuthed(r *http.Request) bool {
	if adminRequestAuthed(r) {
		return true
	}
	return integratorTokenValid(extractDeveloperSecret(r))
}

// requireDeveloperTasksAuth allows POST/GET /api/tasks with admin or developer token.
func requireDeveloperTasksAuth(w http.ResponseWriter, r *http.Request) bool {
	if adminRequestAuthed(r) {
		return true
	}
	if !integratorSelfRegisterEnabled() && developerTokenFromEnv() == "" && (integratorStore == nil || integratorStore.ActiveCount() == 0) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "integrator tokens not configured on this node",
			"code":  "developer_token_unconfigured",
			"hint":  "POST /api/integrator/register when self_register is enabled",
		})
		return false
	}
	if !integratorTokenValid(extractDeveloperSecret(r)) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-developer"`)
		http.Error(w, "developer authentication required", http.StatusUnauthorized)
		return false
	}
	return true
}

// tasksListShowsDetails returns whether GET /api/tasks may include manifest_json.
func tasksListShowsDetails(r *http.Request) bool {
	return adminRequestAuthed(r) || developerRequestAuthed(r)
}
