package workersettle

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackme/internal/netutil"
)

// SUP settle fallback client must refuse redirects (parity with HMC Run).
func TestSUPSettleFallbackClientRefusesRedirect(t *testing.T) {
	var collectorSawToken bool
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Admin-Token") != "" {
			collectorSawToken = true
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer collector.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, collector.URL+"/collect", http.StatusFound)
	}))
	defer redirector.Close()

	// Mirror the RunSUP nil-fallback construction.
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: netutil.DenyAdminTokenRedirect}
	req, err := http.NewRequest(http.MethodPost, redirector.URL+"/api/tx/send", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hackme-Admin-Token", "settle-admin-must-not-leak")
	_, err = client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if collectorSawToken {
		t.Fatal("admin token leaked to redirect target")
	}
}
