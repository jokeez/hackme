package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeveloperTasksAuthRequiresToken(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret")
	t.Setenv("HACKME_DEVELOPER_TOKEN", "dev-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	if requireDeveloperTasksAuth(rec, req) {
		t.Fatal("expected deny without header")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestDeveloperTasksAuthDeveloperHeader(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret")
	t.Setenv("HACKME_DEVELOPER_TOKEN", "dev-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	req.Header.Set("X-Hackme-Developer-Token", "dev-secret")
	rec := httptest.NewRecorder()
	if !requireDeveloperTasksAuth(rec, req) {
		t.Fatal("expected allow with developer header")
	}
}

func TestDeveloperTasksAuthAdminStillWorks(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret")
	t.Setenv("HACKME_DEVELOPER_TOKEN", "dev-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	req.Header.Set("X-Hackme-Admin-Token", "admin-secret")
	rec := httptest.NewRecorder()
	if !requireDeveloperTasksAuth(rec, req) {
		t.Fatal("expected allow with admin header")
	}
}

func TestTasksListShowsDetailsPublic(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret")
	t.Setenv("HACKME_DEVELOPER_TOKEN", "dev-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	if tasksListShowsDetails(req) {
		t.Fatal("public list must be redacted")
	}
}

func TestTasksListShowsDetailsDeveloper(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret")
	t.Setenv("HACKME_DEVELOPER_TOKEN", "dev-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("X-Hackme-Developer-Token", "dev-secret")
	if !tasksListShowsDetails(req) {
		t.Fatal("developer should see manifest")
	}
}
