package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The public security hall (web/site/assets/security-hall.json) is the project's
// payout ledger: researchers read it to check what was credited for their report.
// These checks keep it machine-honest — the headline must never claim less than
// the rows below it, every recent payout must carry an on-chain receipt, and the
// hand-maintained headline cannot silently drift further from the itemised rows.
//
// Current known gap: stats.total_paid_hmc (2277) − itemised rows (2263) = 14 HMC
// of spend that is not itemised yet. Adding the missing entries shrinks the gap;
// raising the headline without entries fails this test.
const securityHallHeadlineGapBudgetHMC = 14.0

// txidRequiredFromDate: the hall records an on-chain txid for every payout made
// after it went live (2026-11-05). Earlier grants predate that practice.
const txidRequiredFromDate = "2026-11-05"

type securityHallFinding struct {
	Title         string  `json:"title"`
	Severity      string  `json:"severity"`
	PaidHMC       float64 `json:"paid_hmc"`
	Date          string  `json:"date"`
	TxID          string  `json:"txid"`
	PayoutAddress string  `json:"payout_address"`
}

type securityHallResearcher struct {
	Handle   string                `json:"handle"`
	Name     string                `json:"name"`
	Findings []securityHallFinding `json:"findings"`
}

type securityHallDoc struct {
	Updated string `json:"updated"`
	Budget  struct {
		Period   string  `json:"period"`
		LimitHMC float64 `json:"limit_hmc"`
	} `json:"budget"`
	Stats struct {
		TotalPaidHMC    float64 `json:"total_paid_hmc"`
		Researchers     int     `json:"researchers"`
		ReportsAccepted int     `json:"reports_accepted"`
	} `json:"stats"`
	Researchers []securityHallResearcher `json:"researchers"`
}

func loadSecurityHall(t *testing.T) securityHallDoc {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("web", "site", "assets", "security-hall.json"))
	if err != nil {
		t.Fatalf("read security hall: %v", err)
	}
	var doc securityHallDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse security hall: %v", err)
	}
	if len(doc.Researchers) == 0 {
		t.Fatal("security hall has no researchers")
	}
	return doc
}

func TestSecurityHallHeadlineCoversItemisedRows(t *testing.T) {
	doc := loadSecurityHall(t)
	rows := 0.0
	for _, r := range doc.Researchers {
		for _, f := range r.Findings {
			rows += f.PaidHMC
		}
	}
	gap := doc.Stats.TotalPaidHMC - rows
	if gap < 0 {
		t.Fatalf("stats.total_paid_hmc (%.2f) is below the itemised rows (%.2f): the public headline must never claim less than the ledger it shows", doc.Stats.TotalPaidHMC, rows)
	}
	if gap > securityHallHeadlineGapBudgetHMC {
		t.Fatalf("stats.total_paid_hmc (%.2f) runs %.2f HMC ahead of the itemised rows (%.2f), budget is %.2f: itemise the missing payouts instead of raising the headline",
			doc.Stats.TotalPaidHMC, gap, rows, securityHallHeadlineGapBudgetHMC)
	}
	if doc.Stats.Researchers != len(doc.Researchers) {
		t.Fatalf("stats.researchers = %d, hall lists %d", doc.Stats.Researchers, len(doc.Researchers))
	}
}

func TestSecurityHallPayoutsCarryOnChainReceipts(t *testing.T) {
	doc := loadSecurityHall(t)
	txRe := regexp.MustCompile(`^[0-9a-f]{64}$`)
	addrRe := regexp.MustCompile(`^HMC-([0-9a-f]{16}|[0-9a-f]{64})$`)
	dateRe := regexp.MustCompile(`^20\d\d-\d\d-\d\d$`)
	sev := map[string]bool{"critical": true, "high": true, "medium": true, "low": true, "informational": true}

	for _, r := range doc.Researchers {
		who := r.Handle
		if who == "" {
			who = r.Name
		}
		for _, f := range r.Findings {
			if f.Title == "" {
				t.Errorf("%s: finding without a title", who)
			}
			if !sev[f.Severity] {
				t.Errorf("%s: %q has unknown severity %q", who, f.Title, f.Severity)
			}
			if !dateRe.MatchString(f.Date) {
				t.Errorf("%s: %q has malformed date %q", who, f.Title, f.Date)
			}
			if f.TxID != "" && !txRe.MatchString(f.TxID) {
				t.Errorf("%s: %q has malformed txid %q", who, f.Title, f.TxID)
			}
			if f.PayoutAddress != "" && !addrRe.MatchString(f.PayoutAddress) {
				t.Errorf("%s: %q has malformed payout address %q", who, f.Title, f.PayoutAddress)
			}
			if f.PaidHMC > 0 && f.Date >= txidRequiredFromDate && f.TxID == "" {
				t.Errorf("%s: %q claims %.2f HMC paid on %s with no txid — record the on-chain receipt", who, f.Title, f.PaidHMC, f.Date)
			}
		}
	}
}

func TestSecurityHallMonthlyCapHoldsForCappedSeverities(t *testing.T) {
	doc := loadSecurityHall(t)
	period := doc.Budget.Period
	if period == "" || doc.Budget.LimitHMC <= 0 {
		t.Skip("no budget period published")
	}
	// program note: High and Critical are paid outside the monthly cap.
	capped := 0.0
	for _, r := range doc.Researchers {
		for _, f := range r.Findings {
			if len(f.Date) < len(period) || f.Date[:len(period)] != period {
				continue
			}
			if f.Severity == "high" || f.Severity == "critical" {
				continue
			}
			capped += f.PaidHMC
		}
	}
	if capped > doc.Budget.LimitHMC {
		t.Fatalf("%s capped-severity spend %.2f HMC exceeds the published limit %.2f HMC", period, capped, doc.Budget.LimitHMC)
	}
}
