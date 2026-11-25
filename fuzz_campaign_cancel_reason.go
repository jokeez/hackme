package main

import (
	"context"
	"strings"
	"time"

	"hackme/internal/logsafe"
)

// cancelReasonMaxLen caps a persisted reason. The text can come from a remote reply, and
// summary_json is served to the campaign's owner.
const cancelReasonMaxLen = 240

// recordCampaignCancelReason writes WHY a campaign was cancelled into its summary.
//
// Both internal cancel paths already knew the reason and sent it only to the log:
// "paid pool order %s refused by the pool (%s)" and the create-failed branch in
// orders_pool_escrow. Logs rotate; the row is what an owner reads and what anyone
// auditing the books a week later has. Measured on 2026-11-25: of 66 cancelled
// campaigns, 8 carried a usable reason (pool_register_error) and 48 carried none at
// all, so answering "who killed this campaign" took fifteen queries across four tables
// and still could not explain most of them.
//
// source says which path cancelled it; reason is the detail. Called before the status
// UPDATE so a reader never sees "cancelled" without an explanation beside it.
func (a *app) recordCampaignCancelReason(ctx context.Context, campaignID, source, reason string) {
	campaignID = strings.TrimSpace(campaignID)
	if a == nil || a.db == nil || campaignID == "" {
		return
	}
	var sumRaw string
	if err := a.db.QueryRowContext(ctx,
		`SELECT COALESCE(summary_json,'{}') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&sumRaw); err != nil {
		return
	}
	sum := parseMapJSON(sumRaw)
	if sum == nil {
		sum = map[string]any{}
	}
	src := strings.TrimSpace(source)
	if src == "" {
		src = "unspecified"
	}
	sum["cancel_source"] = src
	if detail := trimCancelReason(reason); detail != "" {
		sum["cancel_reason"] = detail
	}
	sum["cancelled_at_unix"] = time.Now().Unix()
	_, _ = a.db.ExecContext(ctx,
		`UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`, marshalMapJSON(sum), campaignID)
}

// trimCancelReason passes the text through the log barrier (control characters, length)
// before it becomes a stored, owner-visible field.
func trimCancelReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	// errString (exchange_tickers_api.go) adapts the text to the error logsafe.Err takes.
	reason = logsafe.Err(errString(reason))
	if r := []rune(reason); len(r) > cancelReasonMaxLen {
		reason = string(r[:cancelReasonMaxLen])
	}
	return strings.TrimSpace(reason)
}
