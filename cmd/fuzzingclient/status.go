package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// EscrowSpendView is the honest escrow slice shown by `status`.
type EscrowSpendView struct {
	BudgetHMC         float64 `json:"budget_hmc"`
	RunsPaidHMC       float64 `json:"runs_paid_hmc"`
	BountyPaidHMC     float64 `json:"bounty_paid_hmc"`
	CrashBonusPaidHMC float64 `json:"crash_bonus_paid_hmc,omitempty"`
	BountyLockedHMC   float64 `json:"bounty_locked_hmc"`
	LockedBountyHMC   float64 `json:"locked_bounty_hmc"`
	SpentHMC          float64 `json:"spent_hmc"`
	RefundableHMC     float64 `json:"refundable_hmc,omitempty"`
	RefundedBountyHMC float64 `json:"refunded_bounty_hmc,omitempty"`
	RefundedRunsHMC   float64 `json:"refunded_runs_hmc,omitempty"`
	RefundPath        string  `json:"refund_path,omitempty"`
	Status            string  `json:"status,omitempty"`
}

// OrderStatusView is the aggregated customer-facing status payload.
type OrderStatusView struct {
	OK               bool             `json:"ok"`
	CampaignID       string           `json:"campaign_id"`
	OrderID          string           `json:"order_id,omitempty"`
	CampaignStatus   string           `json:"campaign_status,omitempty"`
	FuzzRunsDone     int              `json:"fuzz_runs_done"`
	FuzzBudgetRuns   int              `json:"fuzz_budget_runs"`
	FuzzProgressPct  float64          `json:"fuzz_progress_pct"`
	PoHProgressCount int              `json:"poh_progress_count,omitempty"`
	PoHTargetSolves  int              `json:"poh_target_solves,omitempty"`
	PoHProgressPct   *float64         `json:"poh_progress_pct,omitempty"`
	PoHStatus        string           `json:"poh_status,omitempty"`
	EtaSecEst        *float64         `json:"eta_sec_est,omitempty"`
	Escrow           *EscrowSpendView `json:"escrow,omitempty"`
	GateURL          string           `json:"gate_url"`
	ReportURL        string           `json:"report_url"`
	PulseURL         string           `json:"pulse_url"`
	PrimaryDeliverable string         `json:"primary_deliverable"`
}

func doStatus(base string, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	campaignID := fs.String("campaign", "", "fuzz campaign id")
	orderID := fs.String("order", "", "optional PoH order / task id")
	reportTok := fs.String("report-token", "", "X-Hackme-Report-Token (or HACKME_REPORT_TOKEN)")
	_ = fs.Parse(args)

	cid := strings.TrimSpace(*campaignID)
	if cid == "" {
		cid = firstPositionalArg(fs.Args())
	}
	if cid == "" {
		return fmt.Errorf("usage: hackme-fuzzing status --campaign CAMPAIGN_ID [--order ORDER_ID] [--report-token TOKEN]")
	}
	oid := strings.TrimSpace(*orderID)
	tok := strings.TrimSpace(*reportTok)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("HACKME_REPORT_TOKEN"))
	}

	pulseRaw, err := fetchJSONAuth(base, tok, "/api/fuzz/campaigns/"+cid+"/pulse")
	if err != nil {
		return fmt.Errorf("pulse: %w", err)
	}
	escrowRaw, err := fetchJSONAuth(base, tok, "/api/fuzz/campaigns/"+cid+"/escrow")
	if err != nil {
		// Escrow may be absent for some campaigns; keep going with pulse-only honesty.
		escrowRaw = nil
		_ = err
	}
	var taskRaw []byte
	if oid != "" {
		taskRaw, err = fetchTaskByID(base, oid)
		if err != nil {
			return fmt.Errorf("order: %w", err)
		}
	}

	view, err := buildOrderStatusView(base, cid, oid, pulseRaw, escrowRaw, taskRaw)
	if err != nil {
		return err
	}
	printJSON(view)
	printStatusHuman(view, tok != "")
	return nil
}

func printStatusHuman(v OrderStatusView, hasToken bool) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Order status (honest):")
	fmt.Fprintf(os.Stderr, "  Fuzz runs:  %.1f%%  (%d / %d)  status=%s\n",
		v.FuzzProgressPct, v.FuzzRunsDone, v.FuzzBudgetRuns, v.CampaignStatus)
	if v.PoHProgressPct != nil {
		fmt.Fprintf(os.Stderr, "  PoH solves: %.1f%%  (%d / %d)  status=%s\n",
			*v.PoHProgressPct, v.PoHProgressCount, v.PoHTargetSolves, v.PoHStatus)
	} else if v.OrderID != "" {
		fmt.Fprintln(os.Stderr, "  PoH solves: (order not found on local /api/tasks yet — pool attach may still be pending)")
	} else {
		fmt.Fprintln(os.Stderr, "  PoH solves: n/a (pass --order for PoH %)")
	}
	if v.Escrow != nil {
		fmt.Fprintf(os.Stderr, "  Escrow:     spent %.4f HMC (runs %.4f + bounty %.4f + crash_bonus %.4f) · locked_bounty %.4f · status=%s\n",
			v.Escrow.SpentHMC, v.Escrow.RunsPaidHMC, v.Escrow.BountyPaidHMC, v.Escrow.CrashBonusPaidHMC, v.Escrow.LockedBountyHMC, v.Escrow.Status)
		if v.Escrow.RefundPath != "" {
			fmt.Fprintf(os.Stderr, "  Refund path: %s (refundable≈%.4f HMC)\n", v.Escrow.RefundPath, v.Escrow.RefundableHMC)
		}
		if v.Escrow.RefundedBountyHMC > 0 || v.Escrow.RefundedRunsHMC > 0 {
			fmt.Fprintf(os.Stderr, "  Refunds:    bounty %.4f · runs %.4f HMC\n",
				v.Escrow.RefundedBountyHMC, v.Escrow.RefundedRunsHMC)
		}
	}
	if v.EtaSecEst != nil {
		fmt.Fprintf(os.Stderr, "  ETA:        ~%s (from pulse rate; not a guarantee)\n", formatDurationSec(*v.EtaSecEst))
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Primary deliverable — CI gate (pass/fail), not finding log spam:")
	if hasToken {
		fmt.Fprintf(os.Stderr, "  curl -sS -H \"X-Hackme-Report-Token: $HACKME_REPORT_TOKEN\" '%s'\n", v.GateURL)
	} else {
		fmt.Fprintf(os.Stderr, "  curl -sS -H \"X-Hackme-Report-Token: <token>\" '%s'\n", v.GateURL)
	}
	fmt.Fprintf(os.Stderr, "  Report: %s\n", v.ReportURL)
}

func formatDurationSec(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	d := time.Duration(sec * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", sec)
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func buildOrderStatusView(base, campaignID, orderID string, pulseRaw, escrowRaw, taskRaw []byte) (OrderStatusView, error) {
	publicBase := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_REPORT_BASE"))
	if publicBase == "" {
		publicBase = strings.TrimRight(base, "/")
	}
	publicBase = strings.TrimRight(publicBase, "/")
	v := OrderStatusView{
		OK:                 true,
		CampaignID:         campaignID,
		OrderID:            orderID,
		GateURL:            publicBase + "/api/fuzz/campaigns/" + campaignID + "/gate?max_critical=0&max_high=0",
		ReportURL:          publicBase + "/api/fuzz/campaigns/" + campaignID + "/report.html",
		PulseURL:           publicBase + "/api/fuzz/campaigns/" + campaignID + "/pulse",
		PrimaryDeliverable: "gate",
	}

	fuzzPct, runsDone, budgetRuns, status, eta, err := parsePulseProgress(pulseRaw)
	if err != nil {
		return v, err
	}
	v.FuzzProgressPct = fuzzPct
	v.FuzzRunsDone = runsDone
	v.FuzzBudgetRuns = budgetRuns
	v.CampaignStatus = status
	v.EtaSecEst = eta

	if len(escrowRaw) > 0 {
		if esc, err := parseEscrowSpend(escrowRaw); err == nil {
			v.Escrow = esc
		}
	}
	if len(taskRaw) > 0 {
		pct, count, target, st, err := parseTaskPoH(taskRaw)
		if err == nil {
			v.PoHProgressPct = &pct
			v.PoHProgressCount = count
			v.PoHTargetSolves = target
			v.PoHStatus = st
		}
	}
	return v, nil
}

func parsePulseProgress(raw []byte) (pct float64, runsDone, budgetRuns int, status string, eta *float64, err error) {
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		return
	}
	status, _ = root["status"].(string)
	prog, _ := root["progress"].(map[string]any)
	if prog == nil {
		err = fmt.Errorf("pulse missing progress")
		return
	}
	runsDone = anyToInt(prog["runs_done"])
	budgetRuns = anyToInt(prog["budget_runs"])
	pct = anyToFloat(prog["progress_pct"])
	if budgetRuns > 0 && pct == 0 && runsDone > 0 {
		pct = (float64(runsDone) / float64(budgetRuns)) * 100
	}
	if v, ok := prog["eta_sec_est"]; ok && v != nil {
		if f := anyToFloat(v); f >= 0 {
			eta = &f
		}
	} else if rate := anyToFloat(prog["runs_per_sec"]); rate > 0 && budgetRuns > runsDone && status == "running" {
		f := float64(budgetRuns-runsDone) / rate
		eta = &f
	}
	return
}

func parseEscrowSpend(raw []byte) (*EscrowSpendView, error) {
	var root struct {
		Escrow map[string]any `json:"escrow"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	e := root.Escrow
	if e == nil {
		// Allow bare escrow object.
		var bare map[string]any
		if err := json.Unmarshal(raw, &bare); err != nil {
			return nil, err
		}
		e = bare
	}
	budget := anyToFloat(e["budget_hmc"])
	runsPaid := anyToFloat(e["runs_paid_hmc"])
	if runsPaid == 0 {
		runsPaid = anyToFloat(e["spent_runs_hmc"])
	}
	bountyPaid := anyToFloat(e["bounty_paid_hmc"])
	crashBonus := anyToFloat(e["crash_bonus_paid_hmc"])
	bountyPool := anyToFloat(e["bounty_pool_hmc"])
	// Prefer server-computed locked_bounty_hmc (accounts for crash bonus + closed state).
	locked := anyToFloat(e["locked_bounty_hmc"])
	if _, ok := e["locked_bounty_hmc"]; !ok {
		locked = bountyPool - bountyPaid - crashBonus
		if locked < 0 {
			locked = 0
		}
	}
	st, _ := e["status"].(string)
	refundPath, _ := e["refund_path"].(string)
	return &EscrowSpendView{
		BudgetHMC:         budget,
		RunsPaidHMC:       runsPaid,
		BountyPaidHMC:     bountyPaid,
		CrashBonusPaidHMC: crashBonus,
		BountyLockedHMC:   locked,
		LockedBountyHMC:   locked,
		SpentHMC:          runsPaid + bountyPaid + crashBonus,
		RefundableHMC:     anyToFloat(e["refundable_hmc"]),
		RefundedBountyHMC: anyToFloat(e["refunded_bounty_hmc"]),
		RefundedRunsHMC:   anyToFloat(e["refunded_runs_hmc"]),
		RefundPath:        refundPath,
		Status:            st,
	}, nil
}

func parseTaskPoH(raw []byte) (pct float64, count, target int, status string, err error) {
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		return
	}
	// Either a single task object or {tasks:[...]} already narrowed.
	task := root
	if t, ok := root["task"].(map[string]any); ok {
		task = t
	}
	count = anyToInt(task["progress_count"])
	target = anyToInt(task["target_solves"])
	pct = anyToFloat(task["progress_pct"])
	if target > 0 && pct == 0 && count > 0 {
		pct = (float64(count) / float64(target)) * 100
	}
	status, _ = task["status"].(string)
	return
}

func fetchJSONAuth(base, reportTok, path string) ([]byte, error) {
	url := strings.TrimRight(base, "/") + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if reportTok != "" {
		req.Header.Set("X-Hackme-Report-Token", reportTok)
	}
	if adm := adminToken(); adm != "" {
		req.Header.Set("X-Hackme-Admin-Token", adm)
	}
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func fetchTaskByID(base, orderID string) ([]byte, error) {
	tok := resolveToken("")
	b, code, err := apiDo(base, tok, http.MethodGet, "/api/tasks", nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		// Retry with admin if available.
		if adm := adminToken(); adm != "" {
			b, code, err = apiDoAdmin(base, adm, http.MethodGet, "/api/tasks", nil)
			if err != nil {
				return nil, err
			}
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("GET /api/tasks HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
	}
	var root struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	for _, t := range root.Tasks {
		id, _ := t["id"].(string)
		if id == orderID {
			return json.Marshal(t)
		}
	}
	return nil, fmt.Errorf("order %q not in /api/tasks", orderID)
}

func anyToInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func anyToFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		return 0
	}
}
