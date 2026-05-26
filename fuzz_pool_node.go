package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/poolfuzz"
)

func poolDistributedCampaign(cfg map[string]any) bool {
	return poolfuzz.PoolDistributed(cfg)
}

func (a *app) syncPoolFuzzCampaign(ctx context.Context, c fuzzAutoCampaign) error {
	coordURL := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/")
	if coordURL == "" {
		coordURL = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL")), "/")
	}
	if coordURL == "" {
		return nil
	}
	token := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ADMIN_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_ADMIN_TOKEN"))
	}
	if token == "" {
		return nil
	}
	cfg := parseMapJSON(c.ConfigJSON)
	var title, desc, ctype string
	_ = a.db.QueryRowContext(ctx,
		`SELECT title, description, campaign_type FROM fuzz_campaigns WHERE id=?`, c.ID).
		Scan(&title, &desc, &ctype)
	if strings.TrimSpace(ctype) == "" {
		ctype = "property"
	}
	body, _ := json.Marshal(map[string]any{
		"id":             c.ID,
		"campaign_type":  ctype,
		"title":          title,
		"description":    desc,
		"status":         "running",
		"budget_runs":    c.BudgetRuns,
		"budget_seconds": c.BudgetSeconds,
		"config":         cfg,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, coordURL+"/api/fuzz/pool/campaigns", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", token)
	cl := &http.Client{Timeout: 30 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("pool register HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
