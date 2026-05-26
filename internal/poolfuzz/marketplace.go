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
		`SELECT id, campaign_type, status, title, budget_runs, summary_json, config_json, created_at, completed_at
		 FROM fuzz_campaigns
		 WHERE json_extract(config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, ctype, status, title, summaryJSON, cfgJSON string
		var budgetRuns int
		var createdAt, completedAt int64
		if err := rows.Scan(&id, &ctype, &status, &title, &budgetRuns, &summaryJSON, &cfgJSON, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		cfg := parseConfigJSON(cfgJSON)
		summary := parseConfigJSON(summaryJSON)
		var findings int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
		budgetHMC := 0.0
		if v, ok := cfg["budget_hmc"]; ok {
			budgetHMC = floatFromJSON(v)
		}
		item := map[string]any{
			"id":             id,
			"campaign_type":  ctype,
			"status":         status,
			"title":          title,
			"budget_runs":    budgetRuns,
			"budget_hmc":     budgetHMC,
			"runs_done":      intFromJSON(summary["runs_done"]),
			"unique_crashes": intFromJSON(summary["unique_crashes"]),
			"findings":       findings,
			"pool":           true,
			"created_at":     createdAt,
			"completed_at":   completedAt,
		}
		if fe, ok := summary["fuzz_engine"].(map[string]any); ok {
			item["check_semantics"] = fe["check_semantics"]
		} else if cs := strings.TrimSpace(jsonString(cfg["check_semantics"])); cs != "" {
			item["check_semantics"] = cs
		}
		out = append(out, item)
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
