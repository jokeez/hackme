package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type coordinatorPoolCampaign struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	RunsDone  int    `json:"runs_done"`
	BudgetRuns int   `json:"budget_runs"`
	Findings  int    `json:"findings"`
}

func (a *app) fetchCoordinatorPoolCampaigns(ctx context.Context) (map[string]coordinatorPoolCampaign, error) {
	base := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	if base == "" {
		return nil, nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/fuzz/pool/campaigns/list?limit=200", nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("coordinator pool list HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Campaigns []coordinatorPoolCampaign `json:"campaigns"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make(map[string]coordinatorPoolCampaign, len(payload.Campaigns))
	for _, c := range payload.Campaigns {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			continue
		}
		out[id] = c
	}
	return out, nil
}

func (a *app) mergeCoordinatorPoolMarketplace(ctx context.Context, items []map[string]any) []map[string]any {
	remote, err := a.fetchCoordinatorPoolCampaigns(ctx)
	if err != nil || len(remote) == 0 {
		return items
	}
	for _, item := range items {
		id, _ := item["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rc, ok := remote[id]
		if !ok {
			continue
		}
		if rc.RunsDone > 0 {
			item["runs_done"] = rc.RunsDone
		}
		if rc.Findings > 0 {
			item["findings"] = rc.Findings
		}
		if st := strings.TrimSpace(rc.Status); st != "" {
			item["status"] = st
		}
		if rc.BudgetRuns > 0 {
			item["budget_runs"] = rc.BudgetRuns
		}
	}
	return items
}

func (a *app) syncPoolCampaignProgressFromCoordinator(ctx context.Context, campaignID string) error {
	remote, err := a.fetchCoordinatorPoolCampaigns(ctx)
	if err != nil {
		return err
	}
	rc, ok := remote[campaignID]
	if !ok || rc.RunsDone <= 0 {
		return nil
	}
	var summaryJSON, status string
	var budgetRuns int
	err = a.db.QueryRowContext(ctx,
		`SELECT status, budget_runs, summary_json FROM fuzz_campaigns WHERE id=?`,
		campaignID).Scan(&status, &budgetRuns, &summaryJSON)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	summary := parseMapJSON(summaryJSON)
	cur := intFromAny(summary["runs_done"])
	if rc.RunsDone <= cur {
		return nil
	}
	summary["runs_done"] = rc.RunsDone
	summary["pool_workers"] = true
	summary["heartbeat_at"] = time.Now().Unix()
	nextStatus := strings.TrimSpace(strings.ToLower(status))
	if budgetRuns > 0 && rc.RunsDone >= budgetRuns {
		nextStatus = "completed"
	} else if st := strings.TrimSpace(strings.ToLower(rc.Status)); st == "completed" {
		nextStatus = "completed"
	}
	completedAt := int64(0)
	if nextStatus == "completed" {
		completedAt = time.Now().Unix()
	}
	_, err = a.db.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?, summary_json=?, completed_at=CASE WHEN ?='completed' AND completed_at=0 THEN ? ELSE completed_at END
		 WHERE id=?`,
		nextStatus, marshalMapJSON(summary), nextStatus, completedAt, campaignID)
	return err
}
