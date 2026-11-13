package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Browsers omit Origin on same-origin GET, so the dashboard asks with POST. A real
// same-origin POST (Origin+Referer+Sec-Fetch-Site) gets the token; a cross-site POST does not.
func TestDesktopLocalAuthBrowserPOST(t *testing.T) {
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("HACKME_ADMIN_TOKEN", "desktop-secret")
	t.Setenv("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", "0")
	call := func(site, origin, referer string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/desktop/local-auth", nil)
		req.RemoteAddr = "127.0.0.1:9"
		req.Host = "127.0.0.1:18590"
		req.Header.Set("Sec-Fetch-Site", site)
		req.Header.Set("Origin", origin)
		req.Header.Set("Referer", referer)
		handleDesktopLocalAuth(rec, req)
		body := map[string]any{}
		_ = json.NewDecoder(rec.Body).Decode(&body)
		return rec.Code, body
	}
	if code, body := call("same-origin", "http://127.0.0.1:18590", "http://127.0.0.1:18590/#orders"); code != 200 || body["admin_token"] != "desktop-secret" {
		t.Fatalf("dashboard POST must get the token, got %d %#v", code, body)
	}
	if code, body := call("cross-site", "https://evil.example", "https://evil.example/x"); code == 200 && body["admin_token"] != nil {
		t.Fatalf("cross-site POST must not get the token: %#v", body)
	}
	if code, body := call("same-site", "http://127.0.0.1:9999", "http://127.0.0.1:9999/"); code == 200 && body["admin_token"] != nil {
		t.Fatalf("another local port must not get the token: %#v", body)
	}
}
