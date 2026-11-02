package poolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hackme/internal/netutil"
)

// SettleOutboxItem is a coordinator-queued fuzz escrow settlement.
type SettleOutboxItem struct {
	ID           int64  `json:"id"`
	CampaignID   string `json:"campaign_id"`
	Kind         string `json:"kind"`
	MinerAddress string `json:"miner_address"`
	Severity     string `json:"severity"`
	CreatedAt    int64  `json:"created_at"`
}

// FetchSettleOutbox returns pending settlements from the coordinator (fair multi-campaign window).
// When a coordinator URL is configured but the admin token is missing, this
// returns an error (not a silent empty success): callers that close escrow
// must not treat "no token" as "fully drained" (report #23).
//
// With OrdersPublishToken set, global list is forbidden — use FetchSettleOutboxForCampaign.
//
// Transient edge failures (HTTP 502/503/504, connection reset) are retried
// with short backoff — bootstrap nodes hit Cloudflare fronts often.
func FetchSettleOutbox(ctx context.Context, limit int) ([]SettleOutboxItem, error) {
	if OrdersPublishToken() != "" {
		return nil, fmt.Errorf("settle outbox: campaign_id required with orders publish token — use FetchSettleOutboxForCampaign")
	}
	return fetchSettleOutboxQuery(ctx, limit, "")
}

// FetchSettleOutboxForCampaign returns pending settle rows for one campaign only.
func FetchSettleOutboxForCampaign(ctx context.Context, campaignID string, limit int) ([]SettleOutboxItem, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("settle outbox: campaign_id required")
	}
	return fetchSettleOutboxQuery(ctx, limit, campaignID)
}

// CountPendingSettleOutbox returns the exact pending count for campaignID.
// Close/finalize must use this — not the fair FetchSettleOutbox window.
func CountPendingSettleOutbox(ctx context.Context, campaignID string) (int64, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return 0, fmt.Errorf("settle outbox: campaign_id required")
	}
	coordURL := ResolveCoordinatorURL()
	token := OrdersCustomerToken()
	if coordURL == "" {
		return 0, nil
	}
	if token == "" {
		return 0, fmt.Errorf("settle outbox: orders publish or coordinator admin token required")
	}
	q := url.Values{}
	q.Set("campaign_id", campaignID)
	q.Set("count", "1")
	reqURL := coordURL + "/api/fuzz/pool/settle/outbox?" + q.Encode()
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(delay):
			}
		}
		n, err := countSettleOutboxOnce(ctx, reqURL, token)
		if err == nil {
			return n, nil
		}
		lastErr = err
		if !settleOutboxTransient(err) {
			return 0, err
		}
	}
	return 0, lastErr
}

func fetchSettleOutboxQuery(ctx context.Context, limit int, campaignID string) ([]SettleOutboxItem, error) {
	coordURL := ResolveCoordinatorURL()
	token := OrdersCustomerToken()
	if coordURL == "" {
		return nil, nil
	}
	if token == "" {
		return nil, fmt.Errorf("settle outbox: orders publish or coordinator admin token required")
	}
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if campaignID != "" {
		q.Set("campaign_id", campaignID)
	}
	reqURL := coordURL + "/api/fuzz/pool/settle/outbox?" + q.Encode()
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		items, err := fetchSettleOutboxOnce(ctx, reqURL, token)
		if err == nil {
			return items, nil
		}
		lastErr = err
		if !settleOutboxTransient(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func countSettleOutboxOnce(ctx context.Context, reqURL, token string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Hackme-Admin-Token", token)
	cli := netutil.HTTPClientNoRedirect(120 * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("settle outbox HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Pending int64 `json:"pending"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return 0, err
	}
	return payload.Pending, nil
}

func fetchSettleOutboxOnce(ctx context.Context, reqURL, token string) ([]SettleOutboxItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hackme-Admin-Token", token)
	cli := netutil.HTTPClientNoRedirect(120 * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("settle outbox HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Items []SettleOutboxItem `json:"items"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

// SettleOutboxErrTransient reports edge/network failures worth retrying.
func SettleOutboxErrTransient(err error) bool {
	return settleOutboxTransient(err)
}

// SettleOutboxErrAuth reports permanent auth failures (do not retry-storm).
func SettleOutboxErrAuth(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden")
}

func settleOutboxTransient(err error) bool {
	if err == nil {
		return false
	}
	if SettleOutboxErrAuth(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "http 502") ||
		strings.Contains(msg, "http 503") ||
		strings.Contains(msg, "http 504") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "broken pipe") {
		return true
	}
	return false
}

// AckSettleOutbox marks coordinator outbox rows applied on the origin node.
// When using the scoped publish token, campaignID is required (hub enforces scope).
func AckSettleOutbox(ctx context.Context, ids []int64) error {
	return AckSettleOutboxForCampaign(ctx, "", ids)
}

// AckSettleOutboxForCampaign ACKs rows; campaignID required when OrdersPublishToken is set.
func AckSettleOutboxForCampaign(ctx context.Context, campaignID string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	coordURL := ResolveCoordinatorURL()
	token := OrdersCustomerToken()
	if coordURL == "" {
		return nil
	}
	if token == "" {
		return fmt.Errorf("settle outbox ack: orders publish or coordinator admin token required")
	}
	if OrdersPublishToken() != "" {
		campaignID = strings.TrimSpace(campaignID)
		if campaignID == "" {
			return fmt.Errorf("settle outbox ack: campaign_id required with orders publish token")
		}
	}
	bodyMap := map[string]any{
		"ids":             ids,
		"confirm_applied": true,
		"origin_applied":  true,
	}
	if campaignID != "" {
		bodyMap["campaign_id"] = campaignID
	}
	body, _ := json.Marshal(bodyMap)
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err := ackSettleOutboxOnce(ctx, coordURL, token, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !settleOutboxTransient(err) {
			return err
		}
	}
	return lastErr
}

func ackSettleOutboxOnce(ctx context.Context, coordURL, token string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		coordURL+"/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	cli := netutil.HTTPClientNoRedirect(120 * time.Second)
	res, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("settle outbox ack HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// AdminToken returns the coordinator admin token from environment.
func AdminToken() string {
	return adminToken()
}
