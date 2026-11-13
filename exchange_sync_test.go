package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hackme/internal/nodecrypto"
)

type fakeExchange struct {
	srv       *httptest.Server
	calls     atomic.Int64
	grantRead bool
	expireOne atomic.Bool
	logins    atomic.Int64
}

func newFakeExchange(t *testing.T, grantRead bool) *fakeExchange {
	t.Helper()
	f := &fakeExchange{grantRead: grantRead}
	id := "0b6f3f0e-6c3a-4c4e-9a54-6a5d1f1c2b3a"
	mux := http.NewServeMux()
	mux.HandleFunc("/desk-api/auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var in struct{ Address string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge_id": id, "address": in.Address, "message": goodMsg(id, in.Address)})
	})
	mux.HandleFunc("/desk-api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.logins.Add(1)
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["scope"] != "read" {
			http.Error(w, "node must ask for read scope", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "exchange_session", Value: "s", Path: "/"})
		scope := "read"
		if !f.grantRead {
			scope = ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "address": in["address"], "scope": scope, "csrf_token": "c"})
	})
	mux.HandleFunc("/desk-api/balances", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if c, err := r.Cookie("exchange_session"); err != nil || c.Value != "s" || f.expireOne.CompareAndSwap(true, false) {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"balances": []map[string]any{
			{"asset": "HMC", "available": 31000000000, "reserved": 4000000000},
			{"asset": "USDT", "available": 5230000000},
			{"asset": "BTC", "available": 30000000000},
		}})
	})
	mux.HandleFunc("/desk-api/deposit/address", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"deposit_address": "HMC-aaaaaaaaaaaaaaaa"})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	old := exchangeLoginAPIBase
	exchangeLoginAPIBase = f.srv.URL + "/desk-api"
	t.Cleanup(func() { exchangeLoginAPIBase = old })
	exchangeSyncState.reset()
	t.Cleanup(exchangeSyncState.reset)
	return f
}

func syncApp(t *testing.T) *app {
	t.Helper()
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	dir := t.TempDir()
	signer, err := nodecrypto.LoadOrCreate(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	return &app{signer: signer, dataDir: dir, rlHits: make(map[string]rateSlot), rlBan: make(map[string]int64)}
}

func localReq(method, path string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:18080"+path, nil)
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:18080"
	r.Header.Set("Origin", "http://127.0.0.1:18080")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func callSync(a *app, r *http.Request) (int, map[string]any) {
	w := httptest.NewRecorder()
	a.handleExchangeAccount(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestExchangeSync_OffByDefault_NoNetwork(t *testing.T) {
	f := newFakeExchange(t, true)
	a := syncApp(t)
	code, out := callSync(a, localReq(http.MethodGet, "/api/exchange/account", nil))
	if code != 200 || out["enabled"] != false || f.calls.Load() != 0 {
		t.Fatalf("code=%d out=%v exchange calls=%d", code, out, f.calls.Load())
	}
}

func TestExchangeSync_EnableShowsBalancesWithoutBTC(t *testing.T) {
	newFakeExchange(t, true)
	a := syncApp(t)
	code, out := callSync(a, localReq(http.MethodPost, "/api/exchange/account/enable", nil))
	if code != 200 || out["ok"] != true || out["deposit_address"] != "HMC-aaaaaaaaaaaaaaaa" {
		t.Fatalf("enable: %d %v", code, out)
	}
	raw, _ := json.Marshal(out["balances"])
	if strings.Contains(string(raw), "BTC") || !strings.Contains(string(raw), `"USDT"`) || !strings.Contains(string(raw), `"reserved":4000000000`) {
		t.Fatalf("balances %s", raw)
	}
}

func TestExchangeSync_RefusesFullSession(t *testing.T) {
	newFakeExchange(t, false) // exchange answers with a full session
	a := syncApp(t)
	_, out := callSync(a, localReq(http.MethodPost, "/api/exchange/account/enable", nil))
	if out["ok"] != false || !strings.Contains(out["error"].(string), "read-only") || out["balances"] != nil && len(out["balances"].([]any)) != 0 {
		t.Fatalf("node must drop a non-read session: %v", out)
	}
}

func TestExchangeSync_ReloginAfterExpiry(t *testing.T) {
	f := newFakeExchange(t, true)
	a := syncApp(t)
	callSync(a, localReq(http.MethodPost, "/api/exchange/account/enable", nil))
	f.expireOne.Store(true)
	_, out := callSync(a, localReq(http.MethodGet, "/api/exchange/account?force=1", nil))
	if out["ok"] != true || f.logins.Load() != 2 {
		t.Fatalf("relogin: %v logins=%d", out, f.logins.Load())
	}
}

func TestExchangeSync_Gates(t *testing.T) {
	newFakeExchange(t, true)
	a := syncApp(t)
	for name, mk := range map[string]func() *http.Request{
		"cross-site read": func() *http.Request {
			return localReq(http.MethodGet, "/api/exchange/account", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
		},
		"cross-site enable": func() *http.Request {
			return localReq(http.MethodPost, "/api/exchange/account/enable", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
		},
		"rebinding host": func() *http.Request {
			r := localReq(http.MethodGet, "/api/exchange/account", nil)
			r.Host = "evil.example:18080"
			return r
		},
		"remote client": func() *http.Request {
			r := localReq(http.MethodGet, "/api/exchange/account", nil)
			r.RemoteAddr = "203.0.113.9:1234"
			return r
		},
	} {
		code, out := callSync(a, mk())
		if code < 400 {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
	if a.exchangeSyncEnabled() {
		t.Fatal("a refused request enabled the sync")
	}
}

func TestExchangeSync_DisableForgetsSession(t *testing.T) {
	f := newFakeExchange(t, true)
	a := syncApp(t)
	callSync(a, localReq(http.MethodPost, "/api/exchange/account/enable", nil))
	code, out := callSync(a, localReq(http.MethodDelete, "/api/exchange/account/enable", nil))
	if code != 200 || out["enabled"] != false || a.exchangeSyncEnabled() {
		t.Fatalf("disable: %d %v", code, out)
	}
	before := f.calls.Load()
	callSync(a, localReq(http.MethodGet, "/api/exchange/account", nil))
	if f.calls.Load() != before {
		t.Fatal("disabled sync still contacts the exchange")
	}
}
