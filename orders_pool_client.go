package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/hunt"
	"hackme/internal/netutil"
	"hackme/internal/poolsync"
)

// ordersHubURL is the public hub origin for Fund confirm + pool Dig/Hunt start (rc20).
// Empty disables the thin-client path (CI / local-only Dig).
func ordersHubURL() string {
	for _, k := range []string{"HACKME_ORDERS_HUB_URL", "HACKME_POOL_HUB_URL"} {
		if u := strings.TrimRight(strings.TrimSpace(os.Getenv(k)), "/"); u != "" {
			return u
		}
	}
	return ""
}

func (a *app) customerPoolOwnerRef() string {
	if a != nil {
		if ref := strings.TrimSpace(a.defaultOrdersOwnerRef()); ref != "" {
			return chain.OrdersPrepaidNormalizeAddr(ref)
		}
	}
	return ""
}

func (a *app) customerPoolAuthHeaders() (http.Header, error) {
	tok := poolsync.OrdersPublishToken()
	if tok == "" {
		return nil, fmt.Errorf("orders publish token not configured")
	}
	owner := a.customerPoolOwnerRef()
	if owner == "" || !chain.IsValidAddress(owner) {
		return nil, fmt.Errorf("orders owner wallet unavailable")
	}
	pub, sig, ts, err := a.signOrdersPoolOwnerProof(owner)
	if err != nil {
		return nil, err
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("X-Hackme-Admin-Token", tok)
	h.Set("X-Hackme-Orders-Owner-Ref", owner)
	h.Set("X-Hackme-Orders-Owner-PubKey", pub)
	h.Set("X-Hackme-Orders-Owner-Sig", sig)
	h.Set("X-Hackme-Orders-Owner-Ts", strconv.FormatInt(ts, 10))
	return h, nil
}

// tryCustomerPoolDigViaHub proxies product Dig to the hub when pool_distributed and hub URL are set.
// Returns true when the request was handled (success or error written).
func (a *app) tryCustomerPoolDigViaHub(w http.ResponseWriter, r *http.Request, req securityAuditRequest) bool {
	poolDist := true
	if req.PoolDistributed != nil {
		poolDist = *req.PoolDistributed
	}
	if !poolDist || ordersHubURL() == "" {
		return false
	}
	// Hub minting node handles pool Dig locally — do not HTTP-loop to self.
	if a.ordersPoolHubCapable() && envBool("HACKME_CHAIN_MINTING_HUB", false) {
		return false
	}
	body, err := json.Marshal(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "encode failed", nil)
		return true
	}
	a.proxyOrdersPoolHub(w, r.Context(), "/api/orders/pool/dig", body)
	return true
}

func (a *app) tryCustomerPoolHuntViaHub(w http.ResponseWriter, r *http.Request, req hunt.CreateRequest) bool {
	if !req.PoolDistributed || ordersHubURL() == "" {
		return false
	}
	if a.ordersPoolHubCapable() && envBool("HACKME_CHAIN_MINTING_HUB", false) {
		return false
	}
	body, err := json.Marshal(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "encode failed", nil)
		return true
	}
	a.proxyOrdersPoolHub(w, r.Context(), "/api/orders/pool/hunt", body)
	return true
}

func (a *app) proxyOrdersPoolHub(w http.ResponseWriter, ctx context.Context, path string, body []byte) {
	hdr, err := a.customerPoolAuthHeaders()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pool_unavailable", err.Error(), nil)
		return
	}
	base := ordersHubURL()
	url := base + path
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "pool_unavailable", err.Error(), nil)
		return
	}
	httpReq.Header = hdr
	resp, err := netutil.HTTPClientNoRedirect(90 * time.Second).Do(httpReq)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "pool_unavailable",
			"hub pool unreachable: "+err.Error(), map[string]any{"hub_url": base})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if mirrorErr := a.mirrorHubPoolCampaignLocal(ctx, raw); mirrorErr != nil {
			// Non-fatal for miners (hub already registered); surface in response.
			var parsed map[string]any
			if json.Unmarshal(raw, &parsed) == nil {
				parsed["local_mirror_warning"] = mirrorErr.Error()
				writeJSON(w, parsed)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(raw)
		return
	}
	var parsed map[string]any
	code := "pool_unavailable"
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &parsed) == nil {
		if c, ok := parsed["code"].(string); ok && c != "" {
			code = c
		}
		if e, ok := parsed["error"].(string); ok && e != "" {
			msg = e
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("hub pool HTTP %d", resp.StatusCode)
	}
	st := resp.StatusCode
	if st < 400 {
		st = http.StatusBadGateway
	}
	writeAPIError(w, st, code, msg, map[string]any{"hub_url": base, "hub_status": resp.StatusCode})
}

// mirrorHubPoolCampaignLocal stores a UI-facing campaign row without local escrow.
func (a *app) mirrorHubPoolCampaignLocal(ctx context.Context, hubJSON []byte) error {
	if a == nil || a.db == nil {
		return nil
	}
	var resp struct {
		OK                    bool           `json:"ok"`
		CampaignID            string         `json:"campaign_id"`
		CustomerReportToken   string         `json:"customer_report_token"`
		OrdersOwnerSecret     string         `json:"orders_owner_secret"`
		Campaign              map[string]any `json:"campaign"`
		HubPool               bool           `json:"hub_pool"`
		PoolDistributed       bool           `json:"pool_distributed"`
	}
	if err := json.Unmarshal(hubJSON, &resp); err != nil {
		return err
	}
	id := strings.TrimSpace(resp.CampaignID)
	if id == "" && resp.Campaign != nil {
		id = strings.TrimSpace(toString(resp.Campaign["id"]))
	}
	if id == "" {
		return nil
	}
	owner := a.customerPoolOwnerRef()
	title := id
	if resp.Campaign != nil {
		if t := strings.TrimSpace(toString(resp.Campaign["title"])); t != "" {
			title = t
		}
	}
	ctype := "property"
	if resp.Campaign != nil {
		if t := strings.TrimSpace(toString(resp.Campaign["campaign_type"])); t != "" {
			ctype = t
		}
	}
	budgetRuns := 64
	budgetSeconds := 3600
	if resp.Campaign != nil {
		if n := intFromAny(resp.Campaign["budget_runs"]); n > 0 {
			budgetRuns = n
		}
		if n := intFromAny(resp.Campaign["budget_seconds"]); n > 0 {
			budgetSeconds = n
		}
	}
	cfg := map[string]any{
		"pool_distributed": true,
		"hub_pool":         true,
		"hub_url":          ordersHubURL(),
		"auto_runner":      "0",
	}
	reportHash := ""
	if tok := strings.TrimSpace(resp.CustomerReportToken); tok != "" {
		reportHash = reportTokenHash(tok)
	}
	now := time.Now().Unix()
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds,
		  config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, ?, 'running', ?, '', ?, '', '', ?, ?, ?, ?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET
		   status='running', config_json=excluded.config_json, summary_json=excluded.summary_json,
		   report_token_hash=CASE WHEN excluded.report_token_hash!='' THEN excluded.report_token_hash ELSE fuzz_campaigns.report_token_hash END`,
		id, ctype, title, owner, budgetRuns, budgetSeconds,
		marshalMapJSON(cfg),
		marshalMapJSON(map[string]any{"hub_pool": true, "pool_register_status": "ok", "execution": "hub_pool"}),
		reportHash, now, now, now)
	if err != nil {
		return err
	}
	if sec := strings.TrimSpace(resp.OrdersOwnerSecret); sec != "" {
		a.persistOrdersOwnerSecret(ctx, id, sec)
	}
	return nil
}

// cancelHubPoolCampaign asks the hub to stop miners and refund unused escrow to prepaid.
func (a *app) cancelHubPoolCampaign(ctx context.Context, campaignID string) (map[string]any, error) {
	base := ordersHubURL()
	if base == "" {
		return nil, fmt.Errorf("HACKME_ORDERS_HUB_URL not set")
	}
	if a.ordersPoolHubCapable() && envBool("HACKME_CHAIN_MINTING_HUB", false) {
		// Already on hub — local cancel path handles escrow.
		return nil, nil
	}
	hdr, err := a.customerPoolAuthHeaders()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"campaign_id": campaignID})
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/api/orders/pool/cancel", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header = hdr
	resp, err := netutil.HTTPClientNoRedirect(90 * time.Second).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hub cancel unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if parsed != nil {
			if e, ok := parsed["error"].(string); ok && e != "" {
				msg = e
			}
		}
		if msg == "" {
			msg = fmt.Sprintf("hub cancel HTTP %d", resp.StatusCode)
		}
		return parsed, fmt.Errorf("%s", msg)
	}
	return parsed, nil
}

// fetchHubOrdersPrepaidHMC reads authoritative tenant prepaid from the hub (rc20 UI).
func (a *app) fetchHubOrdersPrepaidHMC(ctx context.Context) (float64, error) {
	base := ordersHubURL()
	if base == "" {
		return 0, fmt.Errorf("HACKME_ORDERS_HUB_URL not set")
	}
	if a.ordersPoolHubCapable() && envBool("HACKME_CHAIN_MINTING_HUB", false) {
		owner := ""
		if a.signer != nil {
			owner = chain.OrdersPrepaidNormalizeAddr(a.signer.Address())
		}
		return a.chain.GetOrdersPrepaidHMC(ctx, owner)
	}
	hdr, err := a.customerPoolAuthHeaders()
	if err != nil {
		return 0, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/orders/pool/prepaid", nil)
	if err != nil {
		return 0, err
	}
	httpReq.Header = hdr
	resp, err := netutil.HTTPClientNoRedirect(12 * time.Second).Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("hub prepaid HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		OK           bool    `json:"ok"`
		SpendableHMC float64 `json:"spendable_hmc"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, err
	}
	return parsed.SpendableHMC, nil
}

// confirmOrdersFundOnHub credits tenant prepaid on the hub after a proven Fund Orders tx.
func (a *app) confirmOrdersFundOnHub(ctx context.Context, txHash string, amountUnits uint64) error {
	base := ordersHubURL()
	if base == "" {
		return nil
	}
	if a.ordersPoolHubCapable() && envBool("HACKME_CHAIN_MINTING_HUB", false) {
		// Already on hub — local CreditOrdersFundForOwner is handled by fund confirm path.
		return nil
	}
	hdr, err := a.customerPoolAuthHeaders()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"tx_hash":      txHash,
		"amount_units": amountUnits,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/api/orders/pool/fund/confirm", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header = hdr
	resp, err := netutil.HTTPClientNoRedirect(30 * time.Second).Do(httpReq)
	if err != nil {
		return fmt.Errorf("hub fund confirm unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) == nil {
			if e, ok := parsed["error"].(string); ok && e != "" {
				msg = e
			}
		}
		if msg == "" {
			msg = fmt.Sprintf("hub fund confirm HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
