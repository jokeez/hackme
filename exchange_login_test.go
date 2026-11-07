package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/nodecrypto"
)

func exchangeLoginRig(t *testing.T, message func(id, addr string) string) (*app, *nodecrypto.Signer) {
	t.Helper()
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	signer, err := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	id := "0b6f3f0e-6c3a-4c4e-9a54-6a5d1f1c2b3a"
	ex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/desk-api/auth/challenge" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var in struct{ Address string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge_id": id, "address": in.Address, "message": message(id, in.Address)})
	}))
	t.Cleanup(ex.Close)
	oldBase, oldRet := exchangeLoginAPIBase, exchangeLoginReturnURL
	exchangeLoginAPIBase, exchangeLoginReturnURL = ex.URL+"/desk-api", "https://exchange.hackme.tech/"
	t.Cleanup(func() { exchangeLoginAPIBase, exchangeLoginReturnURL = oldBase, oldRet })
	return &app{signer: signer, rlHits: make(map[string]rateSlot), rlBan: make(map[string]int64)}, signer
}

func approveReq(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18080/api/exchange-login/approve", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Host = "127.0.0.1:18080"
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func goodMsg(id, addr string) string {
	return "hackme-exchange-auth|v1|" + id + "|" + addr + "|" + strings.Repeat("a", 32)
}

func TestExchangeLogin_SignsOnlyTheExchangeChallenge(t *testing.T) {
	a, signer := exchangeLoginRig(t, goodMsg)
	w := httptest.NewRecorder()
	a.handleExchangeLoginApprove(w, approveReq(map[string]string{
		"X-HackMe-Exchange-Login": "1", "Origin": "http://127.0.0.1:18080", "Sec-Fetch-Site": "same-origin",
	}))
	if w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	var out struct{ Redirect string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	const prefix = "https://exchange.hackme.tech/#hm-login=v1."
	if !strings.HasPrefix(out.Redirect, prefix) {
		t.Fatalf("redirect %q", out.Redirect)
	}
	p := strings.Split(strings.TrimPrefix(out.Redirect, prefix), ".")
	if len(p) != 4 || p[1] != signer.Address() || p[2] != signer.PublicKeyHex() {
		t.Fatalf("fragment %v", p)
	}
	pub, _ := hex.DecodeString(p[2])
	sig, _ := hex.DecodeString(p[3])
	if !ed25519.Verify(pub, []byte(goodMsg(p[0], signer.Address())), sig) {
		t.Fatal("signature does not verify for the exchange message")
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options %q", got)
	}
}

func TestExchangeLogin_Refusals(t *testing.T) {
	sameOrigin := map[string]string{"X-HackMe-Exchange-Login": "1", "Origin": "http://127.0.0.1:18080", "Sec-Fetch-Site": "same-origin"}
	cases := []struct {
		name    string
		msg     func(id, addr string) string
		headers map[string]string
		mutate  func(r *http.Request)
		desktop string
		want    int
	}{
		{"cross-site page", goodMsg, map[string]string{"X-HackMe-Exchange-Login": "1", "Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, nil, "1", http.StatusForbidden},
		{"other localhost port", goodMsg, map[string]string{"X-HackMe-Exchange-Login": "1", "Origin": "http://127.0.0.1:5199", "Sec-Fetch-Site": "same-site"}, nil, "1", http.StatusForbidden},
		{"no confirm header", goodMsg, map[string]string{"Origin": "http://127.0.0.1:18080", "Sec-Fetch-Site": "same-origin"}, nil, "1", http.StatusForbidden},
		{"dns rebinding host", goodMsg, sameOrigin, func(r *http.Request) { r.Host = "evil.example:18080" }, "1", http.StatusNotFound},
		{"remote client", goodMsg, sameOrigin, func(r *http.Request) { r.RemoteAddr = "203.0.113.5:4000" }, "1", http.StatusNotFound},
		{"not desktop mode", goodMsg, sameOrigin, nil, "0", http.StatusNotFound},
		{"foreign message", func(id, addr string) string { return "transfer 1000 HMC to HMC-ffffffffffffffff" }, sameOrigin, nil, "1", http.StatusBadGateway},
		{"other address", func(id, addr string) string { return goodMsg(id, "HMC-ffffffffffffffff") }, sameOrigin, nil, "1", http.StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := exchangeLoginRig(t, c.msg)
			t.Setenv("HACKME_DESKTOP_MODE", c.desktop)
			r := approveReq(c.headers)
			if c.mutate != nil {
				c.mutate(r)
			}
			w := httptest.NewRecorder()
			a.handleExchangeLoginApprove(w, r)
			if w.Code != c.want {
				t.Fatalf("%s: %d %s (want %d)", c.name, w.Code, w.Body.String(), c.want)
			}
			if strings.Contains(w.Body.String(), "hm-login=") {
				t.Fatal("signature leaked on a refused request")
			}
		})
	}
}

func TestExchangeLogin_PageIsLocalAndUnframeable(t *testing.T) {
	a, signer := exchangeLoginRig(t, goodMsg)
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/exchange-login", nil)
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:18080"
	w := httptest.NewRecorder()
	a.handleExchangeLoginPage(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), signer.Address()) {
		t.Fatalf("page %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("page must not be frameable")
	}
}
