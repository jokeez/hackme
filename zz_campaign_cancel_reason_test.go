package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/store"
)

// cancelReasonTestApp is the minimal app the recorder needs: a database.
func cancelReasonTestApp(t *testing.T) *app {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &app{db: db}
}

// Of 66 cancelled campaigns on 2026-11-25, 8 carried a usable reason and 48 carried none,
// because the two internal cancel paths sent the reason to the log and only the status to
// the row. The owner of a cancelled paid order could not learn why, and neither could
// anyone auditing a week later.
func TestRecordCampaignCancelReason(t *testing.T) {
	a := cancelReasonTestApp(t)
	ctx := context.Background()
	const camp = "camp-cancel-reason-1"
	if _, err := a.db.ExecContext(ctx,
		`INSERT INTO fuzz_campaigns (id, campaign_type, status, title, budget_runs, config_json, summary_json, created_at)
		 VALUES (?, 'hunt', 'running', 't', 64, '{}', '{"pool_register_status":"ok"}', 1)`, camp); err != nil {
		t.Fatal(err)
	}

	a.recordCampaignCancelReason(ctx, camp, "pool_refused", "pool register HTTP 409: campaign id exists")

	var sumRaw string
	if err := a.db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, camp).Scan(&sumRaw); err != nil {
		t.Fatal(err)
	}
	var sum map[string]any
	if err := json.Unmarshal([]byte(sumRaw), &sum); err != nil {
		t.Fatalf("summary is not json: %v (%s)", err, sumRaw)
	}
	if sum["cancel_source"] != "pool_refused" {
		t.Fatalf("cancel_source = %v, want pool_refused", sum["cancel_source"])
	}
	got, _ := sum["cancel_reason"].(string)
	if !strings.Contains(got, "409") {
		t.Fatalf("cancel_reason %q lost the detail", got)
	}
	if sum["cancelled_at_unix"] == nil {
		t.Fatal("no cancelled_at_unix recorded")
	}
	// Existing summary keys must survive: progress and register status are read by the UI.
	if sum["pool_register_status"] != "ok" {
		t.Fatalf("recording the reason dropped pool_register_status: %v", sum)
	}
}

// The text can come from a remote reply and the field is owner-visible, so it must be
// bounded and stripped of control characters.
func TestTrimCancelReason(t *testing.T) {
	if got := trimCancelReason("   "); got != "" {
		t.Fatalf("blank reason became %q", got)
	}
	long := trimCancelReason(strings.Repeat("A", 4096))
	if len([]rune(long)) > cancelReasonMaxLen {
		t.Fatalf("reason kept %d runes, want at most %d", len([]rune(long)), cancelReasonMaxLen)
	}
	if got := trimCancelReason("line one\nline two\ttab"); strings.ContainsAny(got, "\n\t") {
		t.Fatalf("control characters survived: %q", got)
	}
}

// A missing campaign or a nil db must not panic: both call sites run inside sync loops.
func TestRecordCampaignCancelReasonSafeOnMissing(t *testing.T) {
	a := cancelReasonTestApp(t)
	a.recordCampaignCancelReason(context.Background(), "no-such-campaign", "pool_refused", "x")
	var empty *app
	empty.recordCampaignCancelReason(context.Background(), "c", "s", "r")
}
