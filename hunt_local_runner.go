package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"hackme/internal/fuzzartifacts"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

func huntLocalAutorunCampaign(cfg map[string]any) bool {
	if poolDistributedCampaign(cfg) {
		return false
	}
	v := cfg["hunt_local_runner"]
	s := strings.TrimSpace(strings.ToLower(toString(v)))
	return s == "1" || s == "true" || s == "yes"
}

func (a *app) huntLocalAutorunTick(ctx context.Context, c fuzzAutoCampaign, cfg map[string]any, now int64) error {
	summary := parseMapJSON(c.SummaryJSON)
	st := hunt.ParseLocalAutorunState(summary)
	if st.Completed {
		_, err := a.db.ExecContext(ctx,
			`UPDATE fuzz_campaigns
			 SET status='completed', completed_at=CASE WHEN completed_at=0 THEN ? ELSE completed_at END, summary_json=?
			 WHERE id=? AND status<>'completed'`,
			now, marshalMapJSON(hunt.LocalAutorunStateToSummary(summary, st)), c.ID)
		if err != nil {
			return err
		}
		a.tryCloseFuzzEscrowForStatus(ctx, c.ID, "completed")
		return nil
	}
	newSt, rep, err := hunt.LocalAutorunTick(ctx, a.repoRoot(), cfg, st, now)
	if err != nil {
		return err
	}
	summary = hunt.LocalAutorunStateToSummary(summary, newSt)
	if rep != nil {
		summary["verdict"] = rep.Verdict
		summary["hunt_local_last_elapsed_sec"] = rep.ElapsedSec
	}
	status := "running"
	completedAt := int64(0)
	if newSt.Completed {
		status = "completed"
		completedAt = now
	}
	_, err = a.db.ExecContext(ctx,
		`UPDATE fuzz_campaigns
		 SET status=?,
		     started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END,
		     summary_json=?,
		     completed_at=CASE WHEN ?='completed' AND completed_at=0 THEN ? ELSE completed_at END
		 WHERE id=?`,
		status, now, marshalMapJSON(summary), status, completedAt, c.ID)
	if err != nil {
		return err
	}
	if newSt.Completed {
		a.tryCloseFuzzEscrowForStatus(ctx, c.ID, "completed")
	}
	if rep != nil && len(rep.Crashes) > 0 {
		log.Printf("hunt local autorun campaign=%s iter=%d crashes=%d verdict=%s",
			c.ID, newSt.IterationsDone, len(rep.Crashes), strings.TrimSpace(rep.Verdict))
		for _, cf := range rep.Crashes {
			if err := a.insertHuntLocalCrash(ctx, c.ID, cf, now); err != nil {
				log.Printf("hunt local: record crash campaign=%s: %v", c.ID, err)
			}
		}
	}
	return nil
}

// insertHuntLocalCrash stores one local Hunt crash as a campaign finding (report, gate, CI).
// Local crashes used to be logged only, so a local Hunt that found an ASAN bug still reported
// zero crashes and a passing gate. One row per unique input; the sanitizer report is kept as
// the trap so many inputs hitting the same bug group into one family.
func (a *app) insertHuntLocalCrash(ctx context.Context, campaignID string, cf fuzzupstream.CrashFinding, now int64) error {
	raw, err := hex.DecodeString(strings.TrimSpace(cf.InputHex))
	if err != nil || len(raw) == 0 {
		return fmt.Errorf("crash input: %v", err)
	}
	inputSHA := fuzzengine.InputBytesSHA256(raw)
	artifactPath := fuzzartifacts.WriteInputBytes(campaignID, inputSHA, raw)
	class := strings.ToLower(strings.TrimSpace(cf.SanitizerClass))
	sub := strings.TrimSpace(cf.SanitizerSubtype)
	findingType := "crash"
	switch class {
	case "asan", "ubsan", "msan", "lsan":
		findingType = class
	}
	severity := "high"
	if class == "lsan" || class == "ubsan" {
		severity = "medium"
	}
	title := "Crash in " + cf.Title
	if sub != "" {
		title = strings.ToUpper(class) + " " + sub + " in " + cf.Title
	}
	detail := map[string]any{
		"source":            "hunt_local_libfuzzer",
		"input_hex":         fuzzengine.RedactInputForReport(cf.InputHex),
		"input_len":         len(raw),
		"sanitizer_class":   class,
		"sanitizer_subtype": sub,
		"sanitizer_label":   cf.SanitizerLabel,
		"trap":              cf.Tail,
		"timestamp":         now,
	}
	repro := "./<your libFuzzer build> " + artifactPath
	findingID := "finding-hunt-local-" + campaignID + "-" + inputSHA[:16]
	_, err = a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, campaignID, findingType, severity, title, inputSHA, artifactPath, repro, marshalMapJSON(detail), now)
	return err
}
