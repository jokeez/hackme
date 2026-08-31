package main

import (
	"context"
	"log"
	"strings"

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
		return err
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
	if rep != nil && len(rep.Crashes) > 0 {
		log.Printf("hunt local autorun campaign=%s iter=%d crashes=%d verdict=%s",
			c.ID, newSt.IterationsDone, len(rep.Crashes), strings.TrimSpace(rep.Verdict))
	}
	return nil
}
