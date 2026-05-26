package main

import (
	"context"
	"errors"
	"net/http"

	"hackme/internal/chain"
)

func (a *app) handleFuzzCampaignProofBundle(w http.ResponseWriter, r *http.Request, campaignID string) {
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "campaign not found", nil)
		return
	}
	findings, _ := a.queryFuzzFindings(r.Context(), campaignID, 100)
	bundle := map[string]any{
		"ok":         true,
		"campaign":   c,
		"findings":   findings,
		"proof_v":    "fuzz_proof_bundle_v1",
		"report_url": "/api/fuzz/campaigns/" + campaignID + "/report.html",
	}
	if esc, err := a.chain.GetFuzzEscrow(r.Context(), campaignID); err == nil {
		bundle["escrow"] = esc
	} else if !errors.Is(err, chain.ErrFuzzEscrowNotFound) {
		bundle["escrow_error"] = err.Error()
	}
	writeJSON(w, bundle)
}

func (a *app) queryFuzzFindings(ctx context.Context, campaignID string, limit int) ([]fuzzFinding, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at
		 FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at DESC LIMIT ?`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fuzzFinding
	for rows.Next() {
		var f fuzzFinding
		var detail string
		if err := rows.Scan(&f.ID, &f.CampaignID, &f.FindingType, &f.Severity, &f.Title, &f.InputSHA256, &f.Artifact, &f.ReproCmd, &detail, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Detail = parseMapJSON(detail)
		out = append(out, f)
	}
	return out, rows.Err()
}
