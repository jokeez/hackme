package poolfuzz

import (
	"context"
	"fmt"
	"strings"
)

// ListPublicCampaigns returns redacted pool campaigns for the marketplace UI.
func (s *Service) ListPublicCampaigns(ctx context.Context, limit int) ([]map[string]any, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, campaign_type, status, title, owner_ref, budget_runs, summary_json, config_json, created_at, completed_at
		 FROM fuzz_campaigns
		 WHERE json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		   AND status IN ('planned', 'running')
		 ORDER BY created_at DESC
		 LIMIT ?`, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id, ctype, status, title, ownerRef, summaryJSON, cfgJSON string
		var budgetRuns int
		var createdAt, completedAt int64
		if err := rows.Scan(&id, &ctype, &status, &title, &ownerRef, &budgetRuns, &summaryJSON, &cfgJSON, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		cfg := parseConfigJSON(cfgJSON)
		if !IsMarketplaceCampaign(status, id, title, ownerRef, cfg) {
			continue
		}
		summary := parseConfigJSON(summaryJSON)
		var findings int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
		runsDone := runsDoneForCampaign(ctx, s.DB, id, summary)
		displayStatus := status
		if budgetRuns > 0 && runsDone >= budgetRuns && displayStatus == "running" {
			displayStatus = "completed"
		}
		budgetHMC := 0.0
		if v, ok := cfg["budget_hmc"]; ok {
			budgetHMC = floatFromJSON(v)
		}
		item := map[string]any{
			"id":             id,
			"campaign_type":  ctype,
			"status":         displayStatus,
			"title":          title,
			"budget_runs":    budgetRuns,
			"budget_hmc":     budgetHMC,
			"runs_done":      runsDone,
			"unique_crashes": intFromJSON(summary["unique_crashes"]),
			"findings":       findings,
			"pool":           true,
			"created_at":     createdAt,
			"completed_at":   completedAt,
		}
		if fe, ok := summary["fuzz_engine"].(map[string]any); ok {
			item["check_semantics"] = fe["check_semantics"]
			item["depth_tier"] = fe["depth_tier"]
			item["input_mode"] = fe["input_mode"]
		} else if cs := strings.TrimSpace(jsonString(cfg["check_semantics"])); cs != "" {
			item["check_semantics"] = cs
		}
		if dt := strings.TrimSpace(jsonString(cfg["depth_tier"])); dt != "" {
			item["depth_tier"] = dt
		}
		if budgetHMC > 0 && budgetRuns >= 8 {
			item["per_run_hmc"] = (budgetHMC * 0.20) / float64(budgetRuns)
		}
		if native, ok := summary["native"].(map[string]any); ok {
			item["native_status"] = native["status"]
		} else {
			item["native_status"] = "n/a"
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func floatFromJSON(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}
