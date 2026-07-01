package poolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

// FetchSettleOutbox returns pending settlements from the coordinator.
func FetchSettleOutbox(ctx context.Context, limit int) ([]SettleOutboxItem, error) {
	coordURL := ResolveCoordinatorURL()
	token := AdminToken()
	if coordURL == "" || token == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		coordURL+"/api/fuzz/pool/settle/outbox?limit="+strconv.Itoa(limit), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := http.DefaultClient.Do(req)
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

// AckSettleOutbox marks coordinator outbox rows applied on the origin node.
func AckSettleOutbox(ctx context.Context, ids []int64) error {
	coordURL := ResolveCoordinatorURL()
	token := AdminToken()
	if coordURL == "" || token == "" || len(ids) == 0 {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		coordURL+"/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	res, err := http.DefaultClient.Do(req)
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
