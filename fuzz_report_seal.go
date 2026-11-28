package main

import (
	"strings"

	"hackme/internal/fuzzreport"
)

// redactSealedReport strips everything a reader could use to reconstruct the
// uploaded input or the bug site. Counts stay. The payload stays inside the
// box, and only the customer's private key opens it.
func redactSealedReport(c fuzzCampaign, report map[string]any) map[string]any {
	if report == nil || fuzzreport.Pub(c.Config) == nil {
		return report
	}
	report["report_seal"] = "nacl-box-v1"
	if camp, ok := report["campaign"].(fuzzCampaign); ok {
		camp.Description = ""
		camp.Config = sealedConfigView(camp.Config)
		camp.Summary = sealedSummaryView(camp.Summary)
		report["campaign"] = camp
	}
	if rows, ok := report["findings"].([]fuzzFinding); ok {
		report["findings"] = sealedFindingRows(rows)
	}
	for _, key := range []string{"top_issues", "sanitizer_hygiene", "coverage_noise"} {
		if rows, ok := report[key].([]fuzzProductTopIssue); ok {
			report[key] = sealedTopIssues(rows)
		}
	}
	report["dig_depth"] = map[string]any{"withheld": true}
	report["finding_families"] = map[string]any{"withheld": true}
	report["sanitizer_summary"] = map[string]any{"withheld": true}
	report["target_fingerprint"] = map[string]any{"available": false, "note": "withheld"}
	report["baseline_diff"] = map[string]any{"withheld": true}
	report["fuzz_engine"] = map[string]any{"sealed": true}
	if totals, ok := report["totals"].(map[string]any); ok {
		delete(totals, "by_type")
	}
	return report
}

func sealedConfigView(cfg map[string]any) map[string]any {
	out := map[string]any{}
	if cfg == nil {
		return out
	}
	for _, k := range []string{
		"pool_distributed", "hunt_source", "hunt_package", "campaign_type",
		"work_kind", "report_seal_pub", "depth_tier",
	} {
		if v, ok := cfg[k]; ok {
			out[k] = v
		}
	}
	return out
}

func sealedSummaryView(s map[string]any) map[string]any {
	out := map[string]any{}
	if s == nil {
		return out
	}
	for _, k := range []string{"runs_done", "pool_runs_done", "unique_crashes", "crash_count"} {
		if v, ok := s[k]; ok {
			out[k] = v
		}
	}
	return out
}

func sealedFindingRows(rows []fuzzFinding) []fuzzFinding {
	out := make([]fuzzFinding, len(rows))
	for i, f := range rows {
		box := ""
		if f.Detail != nil {
			box, _ = f.Detail["box"].(string)
		}
		f.Title = "sealed"
		f.FindingType = "sealed"
		f.Severity = "sealed"
		f.InputSHA256 = ""
		f.Artifact = ""
		f.ReproCmd = ""
		f.Detail = map[string]any{"seal": "nacl-box-v1"}
		if box != "" {
			f.Detail["box"] = box
		}
		out[i] = f
	}
	return out
}

// sealStoredFinding moves an uploaded finding into the customer box. On a
// seal failure the plaintext is not written.
func sealStoredFinding(campaignID string, cfg map[string]any, title, inputSHA, artifact, repro string, detail map[string]any) (string, string, string, string, string) {
	pub := fuzzreport.Pub(cfg)
	if pub == nil {
		return title, inputSHA, artifact, repro, marshalMapJSON(detail)
	}
	payload := map[string]any{}
	for k, v := range detail {
		payload[k] = v
	}
	payload["title"] = title
	payload["input_sha256"] = inputSHA
	payload["artifact_path"] = artifact
	payload["repro_cmd"] = repro
	env, err := fuzzreport.Envelope(pub, payload, "")
	if err != nil {
		env = `{"seal":"nacl-box-v1"}`
	}
	sha := ""
	if strings.TrimSpace(inputSHA) != "" {
		sha = fuzzreport.InputFingerprint(campaignID, inputSHA)
	}
	return "sealed", sha, "", "", env
}

func sealedTopIssues(rows []fuzzProductTopIssue) []fuzzProductTopIssue {
	out := make([]fuzzProductTopIssue, len(rows))
	for i := range rows {
		out[i] = fuzzProductTopIssue{
			ID:    rows[i].ID,
			Title: "sealed",
		}
	}
	return out
}
