package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hackme/internal/netutil"
	"hackme/internal/poolfuzz"
)

type coordinatorPoolCampaign struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	RunsDone      int    `json:"runs_done"`
	BudgetRuns    int    `json:"budget_runs"`
	Findings      int    `json:"findings"`       // crash-class only (honest)
	FindingsTotal int    `json:"findings_total"` // all rows incl. Dig detector noise
	FailedChecks  int    `json:"failed_checks"`
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
	res, err := netutil.HTTPClientNoRedirect(8 * time.Second).Do(req)
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

// fetchCoordinatorMarketplaceItems loads full public campaign rows from the coordinator.
// Used when the local SQLite marketplace query fails or returns nothing.
func (a *app) fetchCoordinatorMarketplaceItems(ctx context.Context) ([]map[string]any, error) {
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
	res, err := netutil.HTTPClientNoRedirect(8 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("coordinator pool list HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Campaigns []map[string]any `json:"campaigns"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(payload.Campaigns))
	for _, c := range payload.Campaigns {
		if c == nil {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(c["id"]))
		if id == "" || id == "<nil>" {
			continue
		}
		c["id"] = id
		c["pool"] = true
		out = append(out, c)
	}
	return out, nil
}

func (a *app) fetchCoordinatorPoolCampaignProgress(ctx context.Context, campaignID string) (coordinatorPoolCampaign, bool) {
	c, ok, _ := a.fetchCoordinatorPoolCampaignProgressErr(ctx, campaignID)
	return c, ok
}

// stampOrdersOwnerHeader binds shared publish-token reads to the local campaign owner_ref.
func (a *app) stampOrdersOwnerHeader(req *http.Request, campaignID string) {
	if a == nil || a.db == nil || req == nil {
		return
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return
	}
	var owner string
	_ = a.db.QueryRow(`SELECT COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&owner)
	if owner = strings.TrimSpace(owner); owner != "" {
		req.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	}
}

// fetchCoordinatorPoolCampaignProgressErr distinguishes "not on pool yet" (ok=false, err=nil)
// from transport/auth/decode failures (err != nil) so customer gates can fail-closed.
func (a *app) fetchCoordinatorPoolCampaignProgressErr(ctx context.Context, campaignID string) (coordinatorPoolCampaign, bool, error) {
	base := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	if base == "" || campaignID == "" {
		return coordinatorPoolCampaign{}, false, nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/fuzz/pool/campaigns/progress?id="+url.QueryEscape(campaignID), nil)
	if err != nil {
		return coordinatorPoolCampaign{}, false, err
	}
	if tok := coordinatorOrdersCustomerToken(); tok != "" {
		req.Header.Set("X-Hackme-Admin-Token", tok)
	}
	a.stampOrdersOwnerHeader(req, campaignID)
	res, err := netutil.HTTPClientNoRedirect(6 * time.Second).Do(req)
	if err != nil {
		return coordinatorPoolCampaign{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return coordinatorPoolCampaign{}, false, nil
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return coordinatorPoolCampaign{}, false, fmt.Errorf("coordinator progress HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload coordinatorPoolCampaign
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return coordinatorPoolCampaign{}, false, err
	}
	payload.ID = campaignID
	return payload, true, nil
}

func (a *app) fetchCoordinatorFleetCapacity(ctx context.Context) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	if base == "" {
		return nil, nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/fuzz/pool/stats", nil)
	if err != nil {
		return nil, err
	}
	res, err := netutil.HTTPClientNoRedirect(6 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("coordinator pool stats HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if raw, ok := payload["fleet_capacity"].(map[string]any); ok && raw != nil {
		return raw, nil
	}
	// Fallback: top-level fields from /api/fuzz/pool/stats
	out := map[string]any{}
	for _, k := range []string{
		"fleet_hashrate_gh_s", "est_shards_per_hour", "est_dig_runs_per_hour",
		"hybrid_workers_online", "dig_only_workers_online", "workers_online",
		"ghs_priority_enabled", "note",
	} {
		if v, ok := payload[k]; ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func cloneMarketplaceCampaigns(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, src := range in {
		if src == nil {
			continue
		}
		dst := make(map[string]any, len(src)+2)
		for k, v := range src {
			dst[k] = v
		}
		out = append(out, dst)
	}
	return out
}

func (a *app) mergeCoordinatorPoolMarketplace(ctx context.Context, items []map[string]any) []map[string]any {
	remote, err := a.fetchCoordinatorPoolCampaigns(ctx)
	if err != nil {
		remote = map[string]coordinatorPoolCampaign{}
	}
	out := a.mergeCoordinatorPoolMarketplaceWithRemote(ctx, items, remote)
	if len(out) > 0 {
		return out
	}
	// Local DB empty/broken: show coordinator marketplace rows directly.
	full, ferr := a.fetchCoordinatorMarketplaceItems(ctx)
	if ferr != nil || len(full) == 0 {
		return out
	}
	filtered := make([]map[string]any, 0, len(full))
	for _, item := range full {
		st, _ := item["status"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "completed") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(st), "cancelled") {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(item["id"]))
		runsDone := intFromAny(item["runs_done"])
		budgetRuns := intFromAny(item["budget_runs"])
		escrow, _ := item["escrow_status"].(string)
		// Prefer live progress so budget-exhausted coordinator rows with stale
		// summary runs_done=0 do not resurrect as diggable zombies.
		if id != "" {
			if rc, ok := a.fetchCoordinatorPoolCampaignProgress(ctx, id); ok {
				if rc.RunsDone > runsDone {
					runsDone = rc.RunsDone
					item["runs_done"] = rc.RunsDone
				}
				if st2 := strings.TrimSpace(rc.Status); st2 != "" {
					item["status"] = st2
					st = st2
				}
				if rc.BudgetRuns > 0 {
					budgetRuns = rc.BudgetRuns
					item["budget_runs"] = rc.BudgetRuns
				}
			}
		}
		if strings.EqualFold(strings.TrimSpace(st), "completed") || strings.EqualFold(strings.TrimSpace(st), "cancelled") {
			continue
		}
		if !poolfuzz.IsActivelyDiggable(st, escrow, runsDone, budgetRuns) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func (a *app) mergeCoordinatorPoolMarketplaceWithRemote(ctx context.Context, items []map[string]any, remote map[string]coordinatorPoolCampaign) []map[string]any {
	if remote == nil {
		remote = map[string]coordinatorPoolCampaign{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		id = strings.TrimSpace(id)
		rc, ok := remote[id]
		if id != "" && len(items) <= 12 {
			progCtx, progCancel := context.WithTimeout(ctx, 3*time.Second)
			if rc2, ok2 := a.fetchCoordinatorPoolCampaignProgress(progCtx, id); ok2 {
				if !ok || rc2.RunsDone > rc.RunsDone || (rc.RunsDone == 0 && rc2.RunsDone > 0) {
					rc, ok = rc2, true
				}
			}
			progCancel()
		}
		if ok {
			item["runs_done"] = rc.RunsDone
			if rc.Findings > 0 {
				item["findings"] = rc.Findings
			}
			if st := strings.TrimSpace(rc.Status); st != "" {
				item["status"] = st
			}
			if rc.BudgetRuns > 0 {
				item["budget_runs"] = rc.BudgetRuns
			}
			if rd := rc.RunsDone; rc.BudgetRuns > 0 && rd >= rc.BudgetRuns {
				item["status"] = "completed"
			}
			// Persist coordinator terminal status / progress onto the node DB so
			// marketplace does not keep serving local "running" zombies.
			if id != "" && (rc.RunsDone > 0 ||
				strings.EqualFold(strings.TrimSpace(rc.Status), "completed") ||
				strings.EqualFold(strings.TrimSpace(rc.Status), "cancelled")) {
				go func(cid string) {
					syncCtx, syncCancel := context.WithTimeout(context.Background(), 8*time.Second)
					_ = a.syncPoolCampaignProgressFromCoordinator(syncCtx, cid)
					syncCancel()
				}(id)
			}
		}
		st, _ := item["status"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "completed") {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (a *app) syncPoolCampaignProgressFromCoordinator(ctx context.Context, campaignID string) error {
	return a.applyPoolCampaignProgressFromCoordinator(ctx, campaignID, true)
}

// applyPoolCampaignProgressFromCoordinator merges coordinator progress into the local
// campaign row. When closeEscrow is false, escrow is not finalized (used from
// tryCloseFuzzEscrowForStatus to avoid sync→close recursion).
func (a *app) applyPoolCampaignProgressFromCoordinator(ctx context.Context, campaignID string, closeEscrow bool) error {
	rc, ok, fetchErr := a.fetchCoordinatorPoolCampaignProgressErr(ctx, campaignID)
	if fetchErr != nil {
		return fetchErr
	}
	if !ok {
		return nil
	}
	remoteStatus := strings.TrimSpace(strings.ToLower(rc.Status))
	// Always honor terminal coordinator status — even when runs_done is 0 (cancelled /
	// never-started zombies). Previously RunsDone<=0 returned early and left the node
	// marketplace showing "running / ETA warming up" forever with closed escrow.
	if rc.RunsDone <= 0 && remoteStatus != "completed" && remoteStatus != "cancelled" {
		return nil
	}
	var summaryJSON, status string
	var budgetRuns int
	err := a.db.QueryRowContext(ctx,
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
	changed := false
	if rc.RunsDone > cur {
		summary["runs_done"] = rc.RunsDone
		changed = true
	}
	// Crash-class only — never map Dig detector noise / findings_total into unique_crashes
	// (that forced warn_pool_findings + gate FAIL with 0 local crashes).
	if rc.Findings > intFromAny(summary["unique_crashes"]) {
		summary["unique_crashes"] = rc.Findings
		summary["crash_count"] = rc.Findings
		changed = true
	}
	if rc.FailedChecks > intFromAny(summary["failed_checks"]) {
		summary["failed_checks"] = rc.FailedChecks
		changed = true
	}
	if rc.FindingsTotal > intFromAny(summary["findings_total"]) {
		summary["findings_total"] = rc.FindingsTotal
		changed = true
	}
	nextStatus := strings.TrimSpace(strings.ToLower(status))
	if budgetRuns > 0 && rc.RunsDone >= budgetRuns {
		nextStatus = "completed"
	} else if remoteStatus == "completed" || remoteStatus == "cancelled" {
		nextStatus = remoteStatus
	}
	if nextStatus != strings.TrimSpace(strings.ToLower(status)) {
		changed = true
	}
	if !changed {
		if closeEscrow && (nextStatus == "completed" || nextStatus == "cancelled") {
			// Local row already matches the coordinator. Still close escrow:
			// an earlier sync may have written status without a settle.
			a.tryCloseFuzzEscrowForStatus(ctx, campaignID, nextStatus)
		}
		return nil
	}
	summary["pool_workers"] = true
	summary["heartbeat_at"] = time.Now().Unix()
	completedAt := int64(0)
	if nextStatus == "completed" || nextStatus == "cancelled" {
		completedAt = time.Now().Unix()
	}
	_, err = a.db.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?, summary_json=?, completed_at=CASE WHEN ? IN ('completed','cancelled') AND completed_at=0 THEN ? ELSE completed_at END
		 WHERE id=?`,
		nextStatus, marshalMapJSON(summary), nextStatus, completedAt, campaignID)
	if err != nil {
		return err
	}
	if closeEscrow && (nextStatus == "completed" || nextStatus == "cancelled") {
		// Coordinator terminal status is enough to settle. tryClose still
		// refuses finalize when the settle pull fails (report #12).
		a.tryCloseFuzzEscrowForStatus(ctx, campaignID, nextStatus)
	}
	// Best-effort row mirror so customer report.findings[] is not empty when
	// coordinator already recorded crash-class findings.
	if rc.FindingsTotal > 0 || rc.Findings > 0 {
		_, _ = a.pullPoolFindingsFromCoordinator(ctx, campaignID, 100)
	}
	return nil
}

type coordinatorFindingMirrorPayload struct {
	OK       bool `json:"ok"`
	Findings []struct {
		ID          string `json:"id"`
		CampaignID  string `json:"campaign_id"`
		FindingType string `json:"finding_type"`
		Severity    string `json:"severity"`
		Title       string `json:"title"`
		InputSHA256 string `json:"input_sha256"`
		ReproCmd    string `json:"repro_cmd"`
		DetailJSON  string `json:"detail_json"`
		CreatedAt   int64  `json:"created_at"`
	} `json:"findings"`
}

// pullPoolFindingsFromCoordinator mirrors coordinator fuzz_findings into the local
// Orders DB so buildFuzzReport can show rows (not only synced counts).
func (a *app) pullPoolFindingsFromCoordinator(ctx context.Context, campaignID string, limit int) (int, error) {
	base := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	if base == "" || campaignID == "" {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	var localCount int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&localCount)

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	u := fmt.Sprintf("%s/api/fuzz/pool/campaigns/findings?id=%s&limit=%d", base, url.QueryEscape(campaignID), limit)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	tok := coordinatorOrdersCustomerToken()
	if tok == "" {
		return 0, fmt.Errorf("orders publish or coordinator admin token required for findings mirror")
	}
	req.Header.Set("X-Hackme-Admin-Token", tok)
	a.stampOrdersOwnerHeader(req, campaignID)
	res, err := netutil.HTTPClientNoRedirect(12 * time.Second).Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return 0, fmt.Errorf("coordinator findings HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload coordinatorFindingMirrorPayload
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&payload); err != nil {
		return 0, err
	}
	inserted := 0
	now := time.Now().Unix()
	for _, it := range payload.Findings {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			continue
		}
		ftype := strings.TrimSpace(strings.ToLower(it.FindingType))
		if ftype == "" {
			ftype = "interesting_input"
		}
		sev := normalizeFindingSeverity(it.Severity)
		if !allowedFindingSeverity(sev) {
			sev = "info"
		}
		title := sanitizeFindingText(it.Title, 512)
		repro := sanitizeFindingText(it.ReproCmd, 4096)
		inputSHA := strings.TrimSpace(strings.ToLower(it.InputSHA256))
		detail := strings.TrimSpace(it.DetailJSON)
		if len(detail) > 64<<10 {
			detail = detail[:64<<10]
		}
		if detail == "" {
			detail = "{}"
		}
		created := it.CreatedAt
		if created <= 0 {
			created = now
		}
		// Skip natural duplicates already present under a different id.
		var existsNatural int
		_ = a.db.QueryRowContext(ctx,
			`SELECT 1 FROM fuzz_findings
			 WHERE campaign_id=? AND finding_type=? AND input_sha256=? AND title=?
			 LIMIT 1`,
			campaignID, ftype, inputSHA, title).Scan(&existsNatural)
		if existsNatural == 1 {
			continue
		}
		res, err := a.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO fuzz_findings
			 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
			id, campaignID, ftype, sev, title, inputSHA, repro, detail, created)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
			if inputSHA != "" {
				_, _ = a.db.ExecContext(ctx,
					`INSERT INTO fuzz_corpus (campaign_id, input_sha256, first_seen_at, last_seen_at, hits, last_finding_id, artifact_path)
					 VALUES (?, ?, ?, ?, 1, ?, '')
					 ON CONFLICT(campaign_id, input_sha256) DO UPDATE SET
					   last_seen_at=excluded.last_seen_at,
					   hits=fuzz_corpus.hits+1,
					   last_finding_id=excluded.last_finding_id`,
					campaignID, inputSHA, created, created, id)
			}
		}
	}
	if inserted > 0 {
		var summaryJSON string
		if err := a.db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&summaryJSON); err == nil {
			sum := parseMapJSON(summaryJSON)
			sum["pool_findings_mirrored"] = localCount + inserted
			sum["pool_findings_mirror_at"] = now
			sum["pool_findings_mirror_inserted"] = inserted
			_, _ = a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`, marshalMapJSON(sum), campaignID)
		}
	}
	return inserted, nil
}
