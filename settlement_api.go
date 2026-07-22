package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type workerSettlementStateEntry struct {
	SettledHMC     float64 `json:"settled_hmc"`
	SettledSUP     float64 `json:"settled_sup,omitempty"`
	PayoutAddress  string  `json:"payout_address,omitempty"`
	LastTxHash     string  `json:"last_tx_hash,omitempty"`
	LastSettleUnix int64   `json:"last_settle_unix,omitempty"`
}

type workerSettlementMeta struct {
	LastForceUnix int64 `json:"last_force_unix,omitempty"`
}

type workerSettlementState struct {
	Workers map[string]workerSettlementStateEntry `json:"workers"`
	Meta    workerSettlementMeta                  `json:"meta,omitempty"`
}

func workerPayoutMapFromEnv() map[string]string {
	raw := strings.TrimSpace(os.Getenv("HACKME_WORKER_PAYOUT_MAP"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("WORKER_PAYOUT_MAP"))
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" || !strings.Contains(p, "=") {
			continue
		}
		k, v, _ := strings.Cut(p, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func canonicalSettlementStateURL() string {
	if u := strings.TrimSpace(os.Getenv("HACKME_SETTLEMENT_CANONICAL_URL")); u != "" {
		return u
	}
	return "https://hackme.tech/api/settlement/canonical.json"
}

func canonicalSettlementStateFile() string {
	for _, key := range []string{"HACKME_SETTLEMENT_CANONICAL_FILE", "SETTLEMENT_CANONICAL_JSON"} {
		if p := strings.TrimSpace(os.Getenv(key)); p != "" {
			return p
		}
	}
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(workerSettlementStatePath()), "settlement_canonical_public.json"),
		func() string {
			if dd := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dd != "" {
				return filepath.Join(dd, "settlement_canonical_public.json")
			}
			return ""
		}(),
		filepath.Join("data", "settlement_canonical_public.json"),
	} {
		if candidate == "" || candidate == "." {
			continue
		}
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

func readCanonicalSettlementStateFile(path string) (workerSettlementState, error) {
	var out workerSettlementState
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Workers == nil {
		out.Workers = map[string]workerSettlementStateEntry{}
	}
	return out, nil
}

var settlementCanonicalHTTPOnce sync.Once
var settlementCanonicalHTTP *http.Client

func settlementCanonicalHTTPClient() *http.Client {
	settlementCanonicalHTTPOnce.Do(func() {
		settlementCanonicalHTTP = &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				ForceAttemptHTTP2:     false,
				TLSHandshakeTimeout:   3 * time.Second,
				ResponseHeaderTimeout: 2 * time.Second,
			},
		}
	})
	return settlementCanonicalHTTP
}

func fetchCanonicalSettlementStateHTTP(ctx context.Context) (workerSettlementState, error) {
	var out workerSettlementState
	url := canonicalSettlementStateURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", "HackMe-node-settlement/1")
	resp, err := settlementCanonicalHTTPClient().Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("canonical settlement http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Workers == nil {
		out.Workers = map[string]workerSettlementStateEntry{}
	}
	return out, nil
}

func persistCanonicalSettlementSnapshot(st workerSettlementState) {
	p := canonicalSettlementStateFile()
	if p == "" {
		return
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, b, 0o600)
}

// fetchCanonicalSettlementState prefers live canonical HTTP, merges into any local
// snapshot, and refreshes the on-disk cache so unpaid accrual clears after VPS payout.
func fetchCanonicalSettlementState(ctx context.Context) (workerSettlementState, error) {
	var local workerSettlementState
	var haveLocal bool
	if p := canonicalSettlementStateFile(); p != "" {
		if out, err := readCanonicalSettlementStateFile(p); err == nil {
			local = out
			haveLocal = true
		}
	}
	remote, remoteErr := fetchCanonicalSettlementStateHTTP(ctx)
	if remoteErr == nil {
		// HTTP canonical is source of truth. Never merge a stale/poisoned local
		// snapshot upward into the return value — that used to clamp desktop
		// settled_hmc up to inflated cache values, then repairWorkerSettlementState
		// pinned settled==accrued and the UI showed permanent 0 pending.
		go persistCanonicalSettlementSnapshot(remote)
		return remote, nil
	}
	if haveLocal {
		return local, nil
	}
	return workerSettlementState{}, remoteErr
}

// mergeCanonicalSettlementState applies VPS-published settlement rows into local state.
// Returns true when any settled/sup/meta field advanced (caller should persist).
func mergeCanonicalSettlementState(local *workerSettlementState, remote workerSettlementState) bool {
	if local == nil {
		return false
	}
	if local.Workers == nil {
		local.Workers = map[string]workerSettlementStateEntry{}
	}
	changed := false
	for wid, ent := range remote.Workers {
		cur := local.Workers[wid]
		rowChanged := false
		// Fresher VPS settle is authoritative — may lower settled after a poisoned
		// local snapshot previously clamped settled up to accrued.
		if ent.LastSettleUnix > cur.LastSettleUnix {
			if ent.SettledHMC != cur.SettledHMC {
				cur.SettledHMC = ent.SettledHMC
				rowChanged = true
			}
			if ent.SettledSUP != cur.SettledSUP {
				cur.SettledSUP = ent.SettledSUP
				rowChanged = true
			}
			if strings.TrimSpace(ent.PayoutAddress) != "" {
				cur.PayoutAddress = ent.PayoutAddress
			}
			if strings.TrimSpace(ent.LastTxHash) != "" {
				cur.LastTxHash = ent.LastTxHash
			}
			cur.LastSettleUnix = ent.LastSettleUnix
			rowChanged = true
		} else if ent.SettledHMC > cur.SettledHMC {
			cur.SettledHMC = ent.SettledHMC
			rowChanged = true
			if strings.TrimSpace(ent.PayoutAddress) != "" {
				cur.PayoutAddress = ent.PayoutAddress
			}
			if strings.TrimSpace(ent.LastTxHash) != "" {
				cur.LastTxHash = ent.LastTxHash
			}
			if ent.LastSettleUnix > 0 {
				cur.LastSettleUnix = ent.LastSettleUnix
			}
		} else if ent.SettledSUP > cur.SettledSUP {
			cur.SettledSUP = ent.SettledSUP
			rowChanged = true
			if strings.TrimSpace(ent.PayoutAddress) != "" {
				cur.PayoutAddress = ent.PayoutAddress
			}
			if strings.TrimSpace(ent.LastTxHash) != "" {
				cur.LastTxHash = ent.LastTxHash
			}
			if ent.LastSettleUnix > 0 {
				cur.LastSettleUnix = ent.LastSettleUnix
			}
		}
		if rowChanged {
			local.Workers[wid] = cur
			changed = true
		}
	}
	if remote.Meta.LastForceUnix > local.Meta.LastForceUnix {
		local.Meta.LastForceUnix = remote.Meta.LastForceUnix
		changed = true
	}
	return changed
}

func persistWorkerSettlementState(path string, state workerSettlementState) {
	if strings.TrimSpace(path) == "" {
		return
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, b, 0o600)
}

func parseAnyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case uint64:
		return float64(t)
	case uint32:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func workerSettlementStatePath() string {
	if p := strings.TrimSpace(os.Getenv("HACKME_WORKER_SETTLEMENT_STATE_FILE")); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("WORKER_SETTLEMENT_STATE_FILE")); p != "" {
		return p
	}
	if dd := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dd != "" {
		return filepath.Join(dd, "worker_settlement_state.json")
	}
	return filepath.Join("data", "worker_settlement_state.json")
}

func settlementWindowConfigNow() (minSettleHMC float64, dailyForceIntervalSec int64, dailyMinSettleHMC float64) {
	minSettleHMC = 0.01
	if v := strings.TrimSpace(os.Getenv("MIN_SETTLE_HMC")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			minSettleHMC = n
		}
	}
	dailyForceIntervalSec = 86400
	if v := strings.TrimSpace(os.Getenv("DAILY_FORCE_INTERVAL_SEC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			dailyForceIntervalSec = n
		}
	}
	dailyMinSettleHMC = 0.0001
	if v := strings.TrimSpace(os.Getenv("DAILY_MIN_SETTLE_HMC")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			dailyMinSettleHMC = n
		}
	}
	return minSettleHMC, dailyForceIntervalSec, dailyMinSettleHMC
}

func (a *app) handleWorkerSettlement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lite := strings.TrimSpace(r.URL.Query().Get("lite")) == "1"
	base := a.coordinatorBaseURL()
	if base == "" {
		writeJSON(w, map[string]any{
			"ok":     false,
			"reason": "coordinator_not_configured",
		})
		return
	}
	fetchTimeout := 8 * time.Second
	if lite {
		fetchTimeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), fetchTimeout)
	defer cancel()
	details := !lite
	ws, statsStale, err := a.resolveCoordinatorWorkStats(ctx, base, details)
	if err != nil || ws == nil {
		if lite {
			if cached, _, ok := copyCachedWorkStats(workStatsCacheStaleMaxSec); ok && cached != nil {
				ws = cached
				statsStale = true
				err = nil
			}
		}
	}
	if err != nil || ws == nil {
		msg := "coordinator_unavailable"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, map[string]any{
			"ok":      false,
			"reason":  "coordinator_unavailable",
			"message": msg,
			"source":  base,
		})
		return
	}
	statePath := workerSettlementStatePath()
	state := workerSettlementState{Workers: map[string]workerSettlementStateEntry{}}
	if raw, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(raw, &state)
		if state.Workers == nil {
			state.Workers = map[string]workerSettlementStateEntry{}
		}
	}
	canonTimeout := 3 * time.Second
	if lite {
		canonTimeout = 2 * time.Second
	}
	canonCtx, canonCancel := context.WithTimeout(context.Background(), canonTimeout)
	canonMerged := false
	if canon, err := fetchCanonicalSettlementState(canonCtx); err == nil {
		canonMerged = mergeCanonicalSettlementState(&state, canon)
	}
	canonCancel()
	ensureCoordinatorWorkersMap(ws)
	repaired := repairWorkerSettlementState(&state, coordinatorWorkersMap(ws))
	if canonMerged || repaired {
		stateCopy := state
		go persistWorkerSettlementState(statePath, stateCopy)
	}
	workers := coordinatorWorkersMap(ws)
	minSettleHMC, dailyForceIntervalSec, dailyMinSettleHMC := settlementWindowConfigNow()
	minSettleSUP := 0.01
	if v := strings.TrimSpace(os.Getenv("MIN_SETTLE_SUP")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			minSettleSUP = n
		}
	}
	nowUnix := time.Now().Unix()
	lastForceUnix := state.Meta.LastForceUnix
	if lastForceUnix < 0 {
		lastForceUnix = 0
	}
	nextSweepUnix := lastForceUnix + dailyForceIntervalSec
	if nextSweepUnix < nowUnix {
		nextSweepUnix = nowUnix
	}
	etaSec := nextSweepUnix - nowUnix
	if etaSec < 0 {
		etaSec = 0
	}
	coordOmittedBreakdown := len(workers) == 0 && asUint64(ws["workers_count"]) > 0
	payoutMap := workerPayoutMapFromEnv()
	displayWallet := settlementDisplayWalletAddress(a.nodeID, payoutMap)
	walletAccrued, walletSettled, walletUnpaid, accrualSource := walletAccrualFromCoordinator(ws, state.Workers, a.nodeID, a.workerID, payoutMap, a.workerProcessRunning())
	walletAccruedSUP, walletSettledSUP, walletUnpaidSUP := walletAccrualSUPFromCoordinator(ws, state.Workers, a.nodeID, a.workerID, payoutMap)
	desktopWorkerID := strings.TrimSpace(a.workerID)
	if desktopWorkerID == "" {
		desktopWorkerID = "worker-kapa-pc"
	}
	var desktopWorkerAccrued float64
	if row := mapFromAny(workers[desktopWorkerID]); len(row) > 0 {
		desktopWorkerAccrued = parseAnyFloat(row["payout_hmc"])
		if desktopWorkerAccrued < 0 {
			desktopWorkerAccrued = 0
		}
	}
	coordinatorPending := walletUnpaid
	if coordinatorPending <= 0 && desktopWorkerAccrued > 0 {
		coordinatorPending = desktopWorkerAccrued
	}
	var totalAccrued, totalSettled, totalUnpaid float64
	var totalAccruedSUP, totalSettledSUP, totalUnpaidSUP float64
	for workerID, v := range workers {
		if coordOmittedBreakdown && workerID == "worker-active" {
			continue
		}
		row := mapFromAny(v)
		accrued := parseAnyFloat(row["payout_hmc"])
		if accrued < 0 {
			accrued = 0
		}
		settled := state.Workers[workerID].SettledHMC
		if settled < 0 {
			settled = 0
		}
		if settled > accrued {
			settled = accrued
		}
		unpaid := accrued - settled
		totalAccrued += accrued
		totalSettled += settled
		totalUnpaid += unpaid
		accruedSUP := parseAnyFloat(row["payout_sup"])
		if accruedSUP < 0 {
			accruedSUP = 0
		}
		settledSUP := state.Workers[workerID].SettledSUP
		if settledSUP < 0 {
			settledSUP = 0
		}
		if settledSUP > accruedSUP {
			settledSUP = accruedSUP
		}
		totalAccruedSUP += accruedSUP
		totalSettledSUP += settledSUP
		totalUnpaidSUP += accruedSUP - settledSUP
	}
	if strings.TrimSpace(a.nodeID) == "" {
		accrualSource = "fleet_aggregate"
	}
	// All workers routed to one payout wallet via WORKER_PAYOUT_MAP — fleet totals for settlement ops only.
	payoutWalletUnpaidHMC := walletUnpaid
	payoutWalletAccruedHMC := walletAccrued
	payoutWalletUnpaidSUP := walletUnpaidSUP
	if len(payoutMap) > 0 && totalUnpaid >= 0 {
		targets := map[string]struct{}{}
		for _, t := range payoutMap {
			t = strings.TrimSpace(t)
			if strings.HasPrefix(t, "HMC-") {
				targets[strings.ToLower(t)] = struct{}{}
			}
		}
		if len(targets) == 1 {
			payoutWalletAccruedHMC = totalAccrued
			payoutWalletUnpaidHMC = totalUnpaid
			payoutWalletUnpaidSUP = totalUnpaidSUP
			if accrualSource == "none" {
				accrualSource = "unified_payout_map"
			}
		}
	}
	settlementScanSec := int64(120)
	if v := strings.TrimSpace(os.Getenv("SETTLEMENT_TIMER_SEC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			settlementScanSec = n
		}
	}
	nextScanUnix := nowUnix + settlementScanSec
	scanEtaSec := settlementScanSec
	workerBreakdown := map[string]any{}
	for workerID, v := range workers {
		if coordOmittedBreakdown && workerID == "worker-active" {
			continue
		}
		row := mapFromAny(v)
		accrued := parseAnyFloat(row["payout_hmc"])
		if accrued < 0 {
			accrued = 0
		}
		settled := state.Workers[workerID].SettledHMC
		if settled < 0 {
			settled = 0
		}
		if settled > accrued {
			settled = accrued
		}
		accruedSUP := parseAnyFloat(row["payout_sup"])
		if accruedSUP < 0 {
			accruedSUP = 0
		}
		settledSUP := state.Workers[workerID].SettledSUP
		if settledSUP < 0 {
			settledSUP = 0
		}
		if settledSUP > accruedSUP {
			settledSUP = accruedSUP
		}
		workerBreakdown[workerID] = map[string]any{
			"accrued_hmc": accrued,
			"settled_hmc": settled,
			"unpaid_hmc":  accrued - settled,
			"accrued_sup": accruedSUP,
			"settled_sup": settledSUP,
			"unpaid_sup":  accruedSUP - settledSUP,
		}
	}
	desktopWorkerUnpaidHMC := 0.0
	desktopWorkerAccruedHMC := 0.0
	desktopWorkerSettledHMC := 0.0
	if bd, ok := workerBreakdown[desktopWorkerID].(map[string]any); ok {
		desktopWorkerAccruedHMC = parseAnyFloat(bd["accrued_hmc"])
		desktopWorkerSettledHMC = parseAnyFloat(bd["settled_hmc"])
		desktopWorkerUnpaidHMC = parseAnyFloat(bd["unpaid_hmc"])
	}
	// Dashboard: show this PC worker unsettled accrual, not fleet-wide pool total.
	walletAccrued = desktopWorkerAccruedHMC
	walletSettled = desktopWorkerSettledHMC
	walletUnpaid = desktopWorkerUnpaidHMC
	if bd, ok := workerBreakdown[desktopWorkerID].(map[string]any); ok {
		walletAccruedSUP = parseAnyFloat(bd["accrued_sup"])
		walletSettledSUP = parseAnyFloat(bd["settled_sup"])
		walletUnpaidSUP = parseAnyFloat(bd["unpaid_sup"])
	}
	supPolicy, _ := ws["sup_policy"].(map[string]any)
	supNoteCtx, supNoteCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	supNote := a.supSettlementNoteForAPI(supNoteCtx)
	supNoteCancel()
	writeJSON(w, map[string]any{
		"ok":                                    true,
		"lite":                                  lite,
		"source":                                base,
		"coordinator_stats_stale":               statsStale,
		"workers_count":                         len(workers),
		"total_accrued_hmc":                     totalAccrued,
		"total_settled_hmc":                     totalSettled,
		"total_unpaid_hmc":                      totalUnpaid,
		"wallet_accrued_hmc":                    walletAccrued,
		"wallet_settled_hmc":                    walletSettled,
		"wallet_unpaid_hmc":                     walletUnpaid,
		"desktop_worker_unpaid_hmc":             desktopWorkerUnpaidHMC,
		"desktop_worker_accrued_hmc":            desktopWorkerAccruedHMC,
		"payout_wallet_unpaid_hmc":              payoutWalletUnpaidHMC,
		"payout_wallet_accrued_hmc":             payoutWalletAccruedHMC,
		"payout_wallet_unpaid_sup":              payoutWalletUnpaidSUP,
		"wallet_unpaid_scope":                   "desktop_worker",
		"wallet_accrued_sup":                    walletAccruedSUP,
		"wallet_settled_sup":                    walletSettledSUP,
		"wallet_unpaid_sup":                     walletUnpaidSUP,
		"total_accrued_sup":                     totalAccruedSUP,
		"total_unpaid_sup":                      totalUnpaidSUP,
		"coordinator_total_payout_sup":          parseAnyFloat(ws["total_payout_sup"]),
		"sup_policy":                            supPolicy,
		"min_settle_sup":                        minSettleSUP,
		"sup_settlement_note":                   supNote,
		"display_wallet_address":                displayWallet,
		"desktop_worker_id":                     desktopWorkerID,
		"coordinator_raw_payout_hmc":            desktopWorkerAccrued,
		"coordinator_pending_hmc":               coordinatorPending,
		"accrual_source":                        accrualSource,
		"last_signed_miner_address":             strings.TrimSpace(asString(ws["last_signed_miner_address"])),
		"coordinator_total_payout_hmc":          parseAnyFloat(ws["total_payout_hmc"]),
		"fleet_unpaid_hmc":                      totalUnpaid,
		"min_settle_hmc":                        minSettleHMC,
		"daily_min_settle_hmc":                  dailyMinSettleHMC,
		"daily_force_interval_sec":              dailyForceIntervalSec,
		"last_force_unix":                       lastForceUnix,
		"next_daily_sweep_unix":                 nextSweepUnix,
		"daily_sweep_eta_sec":                   etaSec,
		"next_settlement_scan_unix":             nextScanUnix,
		"settlement_scan_eta_sec":               scanEtaSec,
		"settlement_scan_interval_sec":          settlementScanSec,
		"workers_breakdown":                     workerBreakdown,
		"threshold_ready":                       totalUnpaid >= minSettleHMC,
		"coordinator_workers_breakdown_omitted": coordOmittedBreakdown,
		"on_chain_payout_note":                  "UI threshold is not a bank transfer. On-chain payout runs on the VPS/chain host via settle_worker_payouts.sh (ADMIN_TOKEN + CHAIN_BASE + COORD_URL + WORKER_PAYOUT_MAP). Coordinators that omit workers{} require the updated script (synthetic single-map row) or coordinator upgrade.",
		"state_file":                            statePath,
		"updated_unix":                          nowUnix,
	})
}
