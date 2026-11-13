package main

// Wallet ↔ exchange view: the node logs in to exchange.hackme.tech with its wallet key using a
// READ-ONLY session (scope=read: the exchange refuses every order, convert, withdraw and 2FA
// change on it) and shows the account's exchange balances on the Wallet tab.
//
//   GET  /api/exchange/account          cached balances (desktop loopback / admin only)
//   POST /api/exchange/account/enable   opt in (persists data/exchange_sync.on); DELETE opts out
//
// Opt-in: the node contacts the exchange only after the owner turned it on. Nothing here can
// move funds; the exchange session never leaves the node process.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hackme/internal/netutil"
)

const exchangeSyncFlag = "exchange_sync.on"

type exchangeBalance struct {
	Asset     string `json:"asset"`
	Available int64  `json:"available"`
	Reserved  int64  `json:"reserved"`
	Hold      int64  `json:"hold"`
}

type exchangeSync struct {
	mu        sync.Mutex
	client    *http.Client
	loggedIn  bool
	fetchedAt time.Time
	balances  []exchangeBalance
	depAddr   string
	lastErr   string
}

var exchangeSyncState = &exchangeSync{}

// reset drops the session and cached data (opt-out); the mutex itself is never copied.
func (s *exchangeSync) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client, s.loggedIn, s.fetchedAt, s.balances, s.depAddr, s.lastErr = nil, false, time.Time{}, nil, "", ""
}

func (a *app) exchangeSyncEnabled() bool {
	if a.dataDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(a.dataDir, exchangeSyncFlag))
	return err == nil
}

func (s *exchangeSync) httpClient() *http.Client {
	if s.client == nil {
		jar, _ := cookiejar.New(nil)
		c := netutil.HTTPClientNoRedirect(10 * time.Second)
		c.Jar = jar
		s.client = c
	}
	return s.client
}

// login signs a challenge with the wallet key and opens a read-only session.
func (s *exchangeSync) login(ctx context.Context, a *app) error {
	addr := a.signer.Address()
	ch, err := fetchExchangeChallengeWith(ctx, s.httpClient(), addr)
	if err != nil {
		return err
	}
	m := exchangeLoginMsgRE.FindStringSubmatch(ch.Message)
	if m == nil || m[1] != strings.ToLower(ch.ChallengeID) || m[2] != addr {
		return errors.New("unexpected login message from exchange — not signed")
	}
	body, _ := json.Marshal(map[string]string{
		"challenge_id": m[1], "address": addr, "pubkey_ed25519": a.signer.PublicKeyHex(),
		"sig_ed25519": a.signer.SignHex([]byte(ch.Message)), "via": "hackme_wallet", "scope": "read",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeLoginAPIBase+"/auth/verify", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HackMe-Node/exchange-sync")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var out struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("exchange verify HTTP %d", resp.StatusCode)
	}
	if out.Scope != "read" {
		// Never hold a full trading session in the node.
		return errors.New("exchange did not grant a read-only session")
	}
	s.loggedIn = true
	return nil
}

func (s *exchangeSync) get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, exchangeLoginAPIBase+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HackMe-Node/exchange-sync")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// refresh returns cached data (60s) or logs in / fetches anew.
func (s *exchangeSync) refresh(ctx context.Context, a *app, force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && time.Since(s.fetchedAt) < 60*time.Second {
		return
	}
	s.fetchedAt = time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		if !s.loggedIn {
			if err := s.login(ctx, a); err != nil {
				s.lastErr = "login: " + err.Error()
				return
			}
		}
		var b struct {
			Balances []exchangeBalance `json:"balances"`
		}
		code, err := s.get(ctx, "/balances", &b)
		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			s.loggedIn = false
			s.client = nil // drop the stale cookie
			continue
		}
		if err != nil || code != http.StatusOK {
			s.lastErr = fmt.Sprintf("balances: HTTP %d %v", code, err)
			return
		}
		s.balances = b.Balances
		if s.depAddr == "" {
			var d struct {
				DepositAddress string `json:"deposit_address"`
			}
			if c, err := s.get(ctx, "/deposit/address?asset=HMC", &d); err == nil && c == http.StatusOK && strings.HasPrefix(d.DepositAddress, "HMC-") {
				s.depAddr = d.DepositAddress
			}
		}
		s.lastErr = ""
		return
	}
	s.lastErr = "exchange refused the read-only session"
}

// Only assets that exist on the exchange for real: BTC is book-internal, not shown.
var exchangeSyncAssets = map[string]bool{"HMC": true, "SUP": true, "USDT": true}

func (a *app) handleExchangeAccount(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthOrDesktopLoopback(w, r) {
		return
	}
	if a.signer == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "wallet_not_ready", "wallet key not loaded", nil)
		return
	}
	dataPath := ""
	if a.dataDir != "" {
		dataPath = filepath.Join(a.dataDir, exchangeSyncFlag)
	}
	switch r.Method {
	case http.MethodPost, http.MethodDelete:
		if !requireDesktopLoopbackMutating(w, r) {
			return
		}
		if dataPath == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "no_data_dir", "node data dir unknown", nil)
			return
		}
		if r.Method == http.MethodPost {
			if err := os.WriteFile(dataPath, []byte("1\n"), 0o600); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "write_failed", "could not save the setting", nil)
				return
			}
		} else {
			_ = os.Remove(dataPath)
			exchangeSyncState.reset()
		}
	case http.MethodGet:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.exchangeSyncEnabled() {
		writeJSON(w, map[string]any{"ok": true, "enabled": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	exchangeSyncState.refresh(ctx, a, r.URL.Query().Get("force") == "1" || r.Method == http.MethodPost)
	st := exchangeSyncState
	st.mu.Lock()
	defer st.mu.Unlock()
	rows := make([]exchangeBalance, 0, len(st.balances))
	for _, b := range st.balances {
		if exchangeSyncAssets[strings.ToUpper(b.Asset)] {
			rows = append(rows, b)
		}
	}
	writeJSON(w, map[string]any{
		"ok": st.lastErr == "", "enabled": true, "address": a.signer.Address(), "balances": rows,
		"deposit_address": st.depAddr, "fetched_at": st.fetchedAt.UTC().Format(time.RFC3339),
		"error": st.lastErr, "exchange_url": exchangeLoginReturnURL,
	})
}

func fetchExchangeChallengeWith(ctx context.Context, c *http.Client, addr string) (exchangeChallenge, error) {
	body, _ := json.Marshal(map[string]string{"address": addr})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeLoginAPIBase+"/auth/challenge", bytes.NewReader(body))
	if err != nil {
		return exchangeChallenge{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HackMe-Node/exchange-sync")
	resp, err := c.Do(req)
	if err != nil {
		return exchangeChallenge{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return exchangeChallenge{}, fmt.Errorf("exchange challenge HTTP %d", resp.StatusCode)
	}
	var ch exchangeChallenge
	return ch, json.Unmarshal(raw, &ch)
}
