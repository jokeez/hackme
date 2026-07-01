package poolfuzz

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RelaySettler relays fuzz escrow settlements to the campaign origin node, or queues
// pull-mode outbox rows when the origin is loopback-only (desktop behind NAT).
type RelaySettler struct {
	DB               *sql.DB
	Service          *Service
	DefaultOrdersURL string
	AdminToken       func() string
	HTTPClient       *http.Client
}

func (r *RelaySettler) svc() *Service {
	if r.Service != nil {
		return r.Service
	}
	if r.DB != nil {
		return &Service{DB: r.DB}
	}
	return nil
}

func (r *RelaySettler) client() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 12 * time.Second}
}

func (r *RelaySettler) token() string {
	if r.AdminToken == nil {
		return ""
	}
	return strings.TrimSpace(r.AdminToken())
}

func (r *RelaySettler) PayRun(ctx context.Context, campaignID, minerAddress string) error {
	return r.relay(ctx, "run", campaignID, minerAddress, "")
}

func (r *RelaySettler) PayFinding(ctx context.Context, campaignID, minerAddress, severity string) error {
	return r.relay(ctx, "finding", campaignID, minerAddress, severity)
}

func (r *RelaySettler) Finalize(ctx context.Context, campaignID string) error {
	return r.relay(ctx, "finalize", campaignID, "", "")
}

func (r *RelaySettler) relay(ctx context.Context, kind, campaignID, minerAddress, severity string) error {
	s := r.svc()
	if s == nil {
		return nil
	}
	base, pull := r.resolveSettleBase(ctx, campaignID)
	if pull || base == "" {
		return s.EnqueueSettleOutbox(ctx, kind, campaignID, minerAddress, severity)
	}
	tok := r.token()
	if tok == "" {
		return fmt.Errorf("fuzz settle: orders admin token missing")
	}
	body, _ := json.Marshal(map[string]any{
		"kind":          kind,
		"campaign_id":   campaignID,
		"miner_address": minerAddress,
		"severity":      severity,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/fuzz/pool/settle", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	resp, err := r.client().Do(req)
	if err != nil {
		return s.EnqueueSettleOutbox(ctx, kind, campaignID, minerAddress, severity)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict {
		return s.EnqueueSettleOutbox(ctx, kind, campaignID, minerAddress, severity)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fuzz settle HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (r *RelaySettler) resolveSettleBase(ctx context.Context, campaignID string) (base string, pull bool) {
	s := r.svc()
	if s == nil {
		return strings.TrimRight(strings.TrimSpace(r.DefaultOrdersURL), "/"), false
	}
	cfg, err := s.CampaignConfig(ctx, campaignID)
	if err != nil || cfg == nil {
		return strings.TrimRight(strings.TrimSpace(r.DefaultOrdersURL), "/"), false
	}
	if truthy(cfg["orders_settle_pull"]) {
		return "", true
	}
	if v, ok := cfg["orders_settle_base"].(string); ok {
		base = strings.TrimRight(strings.TrimSpace(v), "/")
		if base != "" {
			return base, isLoopbackSettleURL(base)
		}
	}
	def := strings.TrimRight(strings.TrimSpace(r.DefaultOrdersURL), "/")
	return def, isLoopbackSettleURL(def)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "1" || s == "true" || s == "yes"
	case float64:
		return x != 0
	default:
		return false
	}
}

func isLoopbackSettleURL(u string) bool {
	low := strings.ToLower(strings.TrimSpace(u))
	return strings.Contains(low, "127.0.0.1") || strings.Contains(low, "localhost")
}
