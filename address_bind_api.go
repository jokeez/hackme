package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
)

type addressBindRequest struct {
	Address       string `json:"address"`
	PubKeyEd25519 string `json:"pubkey_ed25519"`
	SigEd25519    string `json:"sig_ed25519"`
}

// handleAddressBind binds an address to its spending key from a signed proof (Economics v2
// address/key binding). The canonical chain host applies it; followers relay to it.
func (a *app) handleAddressBind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.allowRate("address_bind:"+clientIP(r), 5) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, map[string]any{"error": "rate limited", "code": "rate_limited"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid body", "code": "invalid_body"})
		return
	}
	var req addressBindRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid json", "code": "invalid_json"})
		return
	}
	if base := a.addressBindRelayBase(); base != "" && !canonicalBaseWouldLoopbackProxy(r, base) {
		code, status, err := postAddressBind(r.Context(), base, req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, map[string]any{"error": "canonical chain unreachable", "code": "canonical_unavailable"})
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		writeJSON(w, map[string]any{"ok": status == http.StatusOK, "status": code, "address": req.Address})
		return
	}
	code, err := a.chain.BindAddressPubKey(r.Context(), req.Address, req.PubKeyEd25519, req.SigEd25519)
	if err != nil {
		if code == "internal_error" {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": "internal error", "code": code})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": err.Error(), "code": code})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "status": code, "address": strings.TrimSpace(req.Address)})
}

// addressBindRelayBase is the canonical chain URL a follower relays binds to ("" = apply locally).
func (a *app) addressBindRelayBase() string {
	if !a.shouldUseCanonicalChainAPI() {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if base == "" || !walletCanonicalBaseUsable(base) || a.canonicalBaseIsSelfNode(base) {
		return ""
	}
	return base
}

// postAddressBind sends a bind proof to base and returns the status code string and HTTP status.
func postAddressBind(ctx context.Context, base string, req addressBindRequest) (string, int, error) {
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/address/bind", bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	// No redirects: a 307/308 would resend the body to a host walletCanonicalBaseUsable never vetted.
	resp, err := netutil.HTTPClientNoRedirect(15 * time.Second).Do(hreq)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var out struct {
		Status string `json:"status"`
		Code   string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out)
	code := out.Status
	if code == "" {
		code = out.Code
	}
	return code, resp.StatusCode, nil
}

// runAddressBindLoop binds this node's wallet key on the canonical chain (desktop/follower nodes).
// It stops once the chain reports the address bound; an unfunded address is retried hourly.
func (a *app) runAddressBindLoop(ctx context.Context) {
	if a == nil || a.signer == nil || !envBool("HACKME_ADDRESS_AUTOBIND", true) {
		return
	}
	addr := strings.TrimSpace(a.signer.Address())
	pub := strings.TrimSpace(a.signer.PublicKeyHex())
	if addr == "" || pub == "" {
		return
	}
	req := addressBindRequest{Address: addr, PubKeyEd25519: pub, SigEd25519: a.signer.SignHex(chain.AddressBindMessage(addr, pub))}
	delay := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		base := a.addressBindRelayBase()
		if base == "" {
			// Canonical host: bind locally (a no-op when its wallet already spent).
			if a.chain == nil {
				return
			}
			code, err := a.chain.BindAddressPubKey(ctx, addr, pub, req.SigEd25519)
			if err == nil || code == "pubkey_binding_mismatch" {
				if code == "pubkey_binding_mismatch" {
					log.Printf("address bind: %s is bound to a different key", addr)
				}
				return
			}
			delay = time.Hour
			continue
		}
		code, status, err := postAddressBind(ctx, base, req)
		switch {
		case err == nil && status == http.StatusOK:
			return
		case code == "pubkey_binding_mismatch":
			log.Printf("address bind: %s is bound to a different key on the canonical chain", addr)
			return
		case code == "unknown_address":
			delay = time.Hour
		case status >= 400 && status < 500 && status != http.StatusTooManyRequests:
			log.Printf("address bind: %s refused by the canonical chain (%s) — not retrying", addr, code)
			return
		default:
			delay = 10 * time.Minute
		}
	}
}
