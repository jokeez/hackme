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
// never a second enqueue after a maybe-paid HTTP attempt. Applied=true only after
// origin ACK / durable outbox applied confirmation.
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

func (r *RelaySettler) PayRun(ctx context.Context, campaignID, minerAddress string, workItemID, reuseOutboxID int64) (SettleResult, error) {
	return r.relay(ctx, "run", campaignID, minerAddress, "", workItemID, reuseOutboxID)
}

func (r *RelaySettler) PayFinding(ctx context.Context, campaignID, minerAddress, severity string, workItemID, reuseOutboxID int64) (SettleResult, error) {
	return r.relay(ctx, "finding", campaignID, minerAddress, severity, workItemID, reuseOutboxID)
}

func (r *RelaySettler) PayCrashBonus(ctx context.Context, campaignID, minerAddress string, workItemID, reuseOutboxID int64) (SettleResult, error) {
	return r.relay(ctx, "crash_bonus", campaignID, minerAddress, "", workItemID, reuseOutboxID)
}

func (r *RelaySettler) Finalize(ctx context.Context, campaignID string, reuseOutboxID int64) (SettleResult, error) {
	return r.relay(ctx, "finalize", campaignID, "", "", 0, reuseOutboxID)
}

// SettleEventID mirrors chain.FuzzSettleEventID for coordinator→origin settle keys.
// Format: outbox:<campaign_id>:<outbox_id> (globally unique; see chain.FuzzSettleEventID).
func SettleEventID(campaignID string, outboxID int64) string {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return fmt.Sprintf("outbox:%d", outboxID)
	}
	return fmt.Sprintf("outbox:%s:%d", campaignID, outboxID)
}

func (r *RelaySettler) relay(ctx context.Context, kind, campaignID, minerAddress, severity string, workItemID, reuseOutboxID int64) (SettleResult, error) {
	s := r.svc()
	if s == nil {
		return SettleResult{}, fmt.Errorf("poolfuzz: no settle service")
	}
	outboxID := reuseOutboxID
	if outboxID > 0 {
		st, err := s.SettleOutboxStatus(ctx, outboxID)
		if err != nil {
			return SettleResult{}, err
		}
		switch st {
		case "applied":
			return SettleResult{OutboxID: outboxID, Applied: true}, nil
		case "pending":
			// reuse same event_id
		default:
			// Missing/unknown — enqueue a fresh durable row.
			outboxID = 0
		}
	}
	if outboxID <= 0 {
		id, err := s.EnqueueSettleOutbox(ctx, kind, campaignID, minerAddress, severity, workItemID)
		if err != nil {
			return SettleResult{}, err
		}
		outboxID = id
	}
	base, pull := r.resolveSettleBase(ctx, campaignID)
	if pull || base == "" {
		return SettleResult{OutboxID: outboxID, Applied: false}, nil
	}
	tok := r.token()
	if tok == "" {
		// Durable outbox row already exists for pull; do not error (avoids re-enqueue).
		return SettleResult{OutboxID: outboxID, Applied: false}, nil
	}
	eventID := SettleEventID(campaignID, outboxID)
	body, _ := json.Marshal(map[string]any{
		"kind":          kind,
		"campaign_id":   campaignID,
		"miner_address": minerAddress,
		"severity":      severity,
		"event_id":      eventID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/fuzz/pool/settle", bytes.NewReader(body))
	if err != nil {
		return SettleResult{OutboxID: outboxID, Applied: false}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	resp, err := r.client().Do(req)
	if err != nil {
		// Timeout / network: leave outbox pending. Do NOT enqueue again.
		return SettleResult{OutboxID: outboxID, Applied: false}, nil
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusOK {
		if _, err := s.AckSettleOutbox(ctx, []int64{outboxID}); err != nil {
			// Origin accepted but local ACK failed — still treat as applied (idempotent event_id).
			return SettleResult{OutboxID: outboxID, Applied: true}, nil
		}
		return SettleResult{OutboxID: outboxID, Applied: true}, nil
	}
	// Non-OK: leave pending for pull drain (same event_id). Never re-enqueue.
	return SettleResult{OutboxID: outboxID, Applied: false}, nil
}

func (r *RelaySettler) resolveSettleBase(ctx context.Context, campaignID string) (base string, pull bool) {
	// Never honor campaign config orders_settle_base — that was an SSRF / admin-token
	// exfil vector (attacker-controlled campaign URL). Settle only to DefaultOrdersURL.
	base = strings.TrimRight(strings.TrimSpace(r.DefaultOrdersURL), "/")
	if !safeSettleBaseURL(base) {
		base = ""
	}
	s := r.svc()
	if s == nil {
		return base, isLoopbackSettleURL(base)
	}
	cfg, err := s.CampaignConfig(ctx, campaignID)
	if err != nil || cfg == nil {
		return base, isLoopbackSettleURL(base)
	}
	if pullVal, ok := cfg["orders_settle_pull"]; ok {
		if truthy(pullVal) {
			return "", true
		}
		// Explicit false: attempt HTTP even for loopback DefaultOrdersURL (tests).
		return base, false
	}
	return base, isLoopbackSettleURL(base)
}

// safeSettleBaseURL rejects non-http(s) and empty schemes so DefaultOrdersURL cannot be
// turned into file:// / gopher:// style SSRF by misconfiguration.
func safeSettleBaseURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return false
	}
	low := strings.ToLower(u)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
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
