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
//
// Always enqueues a durable outbox row first so the event_id is stable. HTTP relay
// carries that event_id; on timeout the pending outbox is left for pull settle —
// never a second enqueue after a maybe-paid HTTP attempt.
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

// SettleEventID mirrors chain.FuzzSettleEventID for coordinator→origin settle keys.
func SettleEventID(outboxID int64) string {
	return fmt.Sprintf("outbox:%d", outboxID)
}

func (r *RelaySettler) relay(ctx context.Context, kind, campaignID, minerAddress, severity string) error {
	s := r.svc()
	if s == nil {
		return nil
	}
	outboxID, err := s.EnqueueSettleOutbox(ctx, kind, campaignID, minerAddress, severity)
	if err != nil {
		return err
	}
	base, pull := r.resolveSettleBase(ctx, campaignID)
	if pull || base == "" {
		return nil
	}
	tok := r.token()
	if tok == "" {
		// Durable outbox row already exists for pull; do not error (avoids re-enqueue).
		return nil
	}
	eventID := SettleEventID(outboxID)
	body, _ := json.Marshal(map[string]any{
		"kind":          kind,
		"campaign_id":   campaignID,
		"miner_address": minerAddress,
		"severity":      severity,
		"event_id":      eventID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/fuzz/pool/settle", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	resp, err := r.client().Do(req)
	if err != nil {
		// Timeout / network: leave outbox pending. Do NOT enqueue again.
		return nil
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusOK {
		_, _ = s.AckSettleOutbox(ctx, []int64{outboxID})
	}
	// Non-OK: leave pending for pull drain (same event_id). Never re-enqueue.
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
	if v, ok := cfg["orders_settle_base"].(string); ok {
		base = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(r.DefaultOrdersURL), "/")
	}
	if pullVal, ok := cfg["orders_settle_pull"]; ok {
		if truthy(pullVal) {
			return "", true
		}
		// Explicit false: attempt HTTP even for loopback bases (tests / rare local origin).
		return base, false
	}
	return base, isLoopbackSettleURL(base)
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
