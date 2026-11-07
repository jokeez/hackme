package main

// "Sign in with HackMe" — log in to exchange.hackme.tech with this node's wallet key.
//
//   GET  /exchange-login              local confirmation page (desktop mode, loopback only)
//   POST /api/exchange-login/approve  same-origin from that page: fetch a login challenge for
//                                     this wallet's address, check it is EXACTLY an exchange
//                                     login message, sign it, return the redirect URL.
//
// The node is not a signing oracle: it signs only "hackme-exchange-auth|v1|<uuid>|<own
// address>|<32 hex>" that it fetched itself, and the redirect target is fixed in code, so a
// phishing page can open the confirmation page but never receive the signature. The page cannot
// be framed (clickjacking) and the approve call needs the desktop loopback + same-origin gates
// that already protect wallet sends.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"hackme/internal/netutil"
)

var (
	// Fixed in code (tests override these package vars; there is no env override).
	exchangeLoginAPIBase   = "https://exchange.hackme.tech/desk-api"
	exchangeLoginReturnURL = "https://exchange.hackme.tech/"
)

var exchangeLoginMsgRE = regexp.MustCompile(`^hackme-exchange-auth\|v1\|([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\|(HMC-[0-9a-f]{16})\|[0-9a-f]{32}$`)

func exchangeLoginGate(w http.ResponseWriter, r *http.Request) bool {
	if !envBool("HACKME_DESKTOP_MODE", false) || !requestFromLoopback(r) || !requestHostIsLoopbackLiteral(r) {
		http.NotFound(w, r)
		return false
	}
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	return true
}

func (a *app) handleExchangeLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !exchangeLoginGate(w, r) {
		return
	}
	if a.signer == nil {
		http.Error(w, "wallet not ready", http.StatusServiceUnavailable)
		return
	}
	addr := html.EscapeString(a.signer.Address())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, exchangeLoginPageHTML, addr, addr)
}

type exchangeChallenge struct {
	ChallengeID string `json:"challenge_id"`
	Address     string `json:"address"`
	Message     string `json:"message"`
}

func (a *app) handleExchangeLoginApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !exchangeLoginGate(w, r) || !requireDesktopLoopbackMutating(w, r) {
		return
	}
	// A plain browser form cannot set this header cross-origin; the page's own fetch does.
	if r.Header.Get("X-HackMe-Exchange-Login") != "1" {
		writeAPIError(w, http.StatusForbidden, "confirm_required", "approve from the HackMe dashboard page", nil)
		return
	}
	if a.signer == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "wallet_not_ready", "wallet key not loaded", nil)
		return
	}
	if !a.allowRate("exchange_login:"+clientIP(r), 2) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many login approvals", nil)
		return
	}
	addr := a.signer.Address()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	ch, err := fetchExchangeChallenge(ctx, addr)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "exchange_unreachable", "could not reach the exchange — check your connection and try again", nil)
		return
	}
	m := exchangeLoginMsgRE.FindStringSubmatch(ch.Message)
	if m == nil || m[1] != strings.ToLower(ch.ChallengeID) || m[2] != addr || !strings.EqualFold(ch.Address, addr) {
		writeAPIError(w, http.StatusBadGateway, "unexpected_challenge", "the exchange returned an unexpected login message — not signed", nil)
		return
	}
	sig := a.signer.SignHex([]byte(ch.Message))
	frag := strings.Join([]string{"v1", m[1], addr, a.signer.PublicKeyHex(), sig}, ".")
	writeJSON(w, map[string]any{"ok": true, "redirect": exchangeLoginReturnURL + "#hm-login=" + frag})
}

func fetchExchangeChallenge(ctx context.Context, addr string) (exchangeChallenge, error) {
	body, _ := json.Marshal(map[string]string{"address": addr})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeLoginAPIBase+"/auth/challenge", bytes.NewReader(body))
	if err != nil {
		return exchangeChallenge{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HackMe-Node/exchange-login")
	resp, err := netutil.HTTPClientNoRedirect(10 * time.Second).Do(req)
	if err != nil {
		return exchangeChallenge{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return exchangeChallenge{}, fmt.Errorf("exchange challenge HTTP %d", resp.StatusCode)
	}
	var ch exchangeChallenge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return exchangeChallenge{}, err
	}
	return ch, nil
}

const exchangeLoginPageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Log in to HackMe Exchange</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b1220;color:#e6edf6;font:15px/1.5 system-ui,sans-serif}
.card{max-width:440px;margin:16px;padding:28px;border:1px solid #23304a;border-radius:14px;background:#111a2e}
h1{font-size:20px;margin:0 0 12px}.addr{font-family:ui-monospace,monospace;background:#0b1220;padding:8px 10px;border-radius:8px;word-break:break-all}
.warn{color:#f5c26b;font-size:13px}.row{display:flex;gap:10px;margin-top:20px}
button{flex:1;padding:11px;border-radius:9px;border:0;font-weight:600;cursor:pointer}
#ok{background:#22c3e6;color:#04121c}#no{background:#23304a;color:#e6edf6}#msg{margin-top:14px;font-size:13px;min-height:1.2em}
</style></head><body><div class="card">
<h1>Log in to exchange.hackme.tech?</h1>
<p>The exchange will open as your HackMe wallet:</p>
<p class="addr">%s</p>
<p class="warn">Only approve if you just pressed "Sign in with HackMe" on exchange.hackme.tech. This logs you in; it cannot move coins, and exchange withdrawals still need your 2FA code.</p>
<div class="row"><button id="no" type="button">Cancel</button><button id="ok" type="button">Log in</button></div>
<p id="msg" role="status"></p></div>
<script>
const ok=document.getElementById('ok'),no=document.getElementById('no'),msg=document.getElementById('msg');
no.onclick=()=>{msg.textContent='Cancelled — you can close this tab.';ok.disabled=no.disabled=true;};
ok.onclick=async()=>{ok.disabled=no.disabled=true;msg.textContent='Signing in…';
 try{const r=await fetch('/api/exchange-login/approve',{method:'POST',headers:{'X-HackMe-Exchange-Login':'1'},credentials:'same-origin'});
  const j=await r.json();if(!r.ok||!j.redirect){throw new Error(j.error||j.message||('HTTP '+r.status));}
  location.replace(j.redirect);}catch(e){msg.textContent='Could not log in: '+e.message;ok.disabled=no.disabled=false;}};
document.title='Log in to HackMe Exchange — %s';
</script></body></html>`
