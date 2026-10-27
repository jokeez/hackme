package workersettle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
)

// Config drives one settle pass (env-compatible with settle_worker_payouts.sh).
type Config struct {
	CoordURL              string
	ChainBase             string
	AdminToken            string
	CoordAdminToken       string
	StateJSON             string
	StateDB               string
	MinSettleUnits        uint64
	DailyMinSettleUnits   uint64
	DailyForceIntervalSec int64
	ForceSettleAll        bool
	WorkerPayoutMap       map[string]string
	Allowlist             map[string]struct{}
	TreasuryReserveUnits  uint64
	MaxChunkUnits         uint64
	TxWaitSec             int
	Sequential            bool
	PayoutPauseSec        int
	NonceRetries          int
	HTTPTimeoutSec        int
	ClearPending          bool
	ExpectedHostname      string
	FeeUnits              uint64
	PublishScript         string // optional absolute path; empty skips
	Log                   *log.Logger
}

// Result summarizes one Run.
type Result struct {
	SettledAny  bool
	PayoutsSent int
	ForceMode   bool
	SkippedLock bool
}

// DefaultConfig fills env-compatible defaults.
func DefaultConfig() Config {
	return Config{
		CoordURL:              envOr("COORD_URL", "http://127.0.0.1:18081"),
		ChainBase:             envOr("CHAIN_BASE", "http://127.0.0.1:18080"),
		AdminToken:            firstNonEmpty(os.Getenv("ADMIN_TOKEN"), os.Getenv("HACKME_ADMIN_TOKEN")),
		CoordAdminToken:       firstNonEmpty(os.Getenv("COORD_ADMIN_TOKEN"), os.Getenv("HACKME_COORDINATOR_ADMIN_TOKEN"), os.Getenv("COORD_TOKEN")),
		StateJSON:             envOr("STATE_FILE", "data/worker_settlement_state.json"),
		MinSettleUnits:        HMCToUnits(envFloat("MIN_SETTLE_HMC", 0.0001)),
		DailyMinSettleUnits:   HMCToUnits(envFloat("DAILY_MIN_SETTLE_HMC", 0.0001)),
		DailyForceIntervalSec: int64(envInt("DAILY_FORCE_INTERVAL_SEC", 86400)),
		ForceSettleAll:        envBool("FORCE_SETTLE_ALL"),
		TreasuryReserveUnits:  HMCToUnits(envFloat("TREASURY_RESERVE_HMC", 0.05)),
		MaxChunkUnits:         HMCToUnits(envFloat("SETTLE_MAX_CHUNK_HMC", 40)),
		TxWaitSec:             envInt("SETTLE_TX_WAIT_SEC", 90),
		Sequential:            envOr("SETTLE_SEQUENTIAL", "1") != "0",
		PayoutPauseSec:        envInt("SETTLE_PAYOUT_PAUSE_SEC", 10),
		NonceRetries:          envInt("SETTLE_NONCE_RETRIES", 6),
		HTTPTimeoutSec:        envInt("CURL_MAX_TIME", 60),
		ClearPending:          envBool("CLEAR_PENDING_HMC") || envBool("CLEAR_PENDING_SETTLE"),
		ExpectedHostname:      strings.TrimSpace(os.Getenv("SETTLE_EXPECTED_HOSTNAME")),
		FeeUnits:              chain.DefaultTransferMinFee,
		WorkerPayoutMap:       parsePayoutMap(os.Getenv("WORKER_PAYOUT_MAP")),
		Allowlist:             parseAllowlist(firstNonEmpty(os.Getenv("SETTLE_WORKER_ALLOWLIST"), os.Getenv("HACKME_SETTLE_WORKER_ALLOWLIST"))),
	}
}

// ResolveStateDB picks SQLite path beside JSON when unset.
func (c *Config) ResolveStateDB() {
	if strings.TrimSpace(c.StateDB) != "" {
		return
	}
	if v := strings.TrimSpace(firstNonEmpty(os.Getenv("STATE_DB"), os.Getenv("HACKME_WORKER_SETTLEMENT_DB"))); v != "" {
		c.StateDB = v
		return
	}
	if os.Getenv("SETTLE_SQLITE") == "0" {
		return
	}
	sf := c.StateJSON
	if strings.HasSuffix(sf, ".json") {
		c.StateDB = strings.TrimSuffix(sf, ".json") + ".db"
	} else {
		c.StateDB = sf + ".db"
	}
}

func (c *Config) logger() *log.Logger {
	if c.Log != nil {
		return c.Log
	}
	return log.New(os.Stderr, "[settle-workers] ", 0)
}

// Run executes one settlement pass against coordinator stats + chain.
func Run(ctx context.Context, cfg Config) (Result, error) {
	cfg.ResolveStateDB()
	lg := cfg.logger()
	var out Result

	if err := cfg.checkHostname(); err != nil {
		return out, err
	}
	if err := cfg.loadTokensFromSecrets(); err != nil {
		return out, err
	}
	if strings.TrimSpace(cfg.AdminToken) == "" {
		return out, fmt.Errorf("ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required")
	}
	if strings.TrimSpace(cfg.CoordAdminToken) == "" {
		return out, fmt.Errorf("COORD_ADMIN_TOKEN required for /api/work/stats?details=1")
	}
	if strings.TrimSpace(cfg.StateDB) == "" {
		return out, fmt.Errorf("STATE_DB required (SQLite ledger); set HACKME_WORKER_SETTLEMENT_DB or STATE_FILE")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.StateJSON), 0o700); err != nil && filepath.Dir(cfg.StateJSON) != "." {
		return out, err
	}
	unlock, held, err := tryFlock(cfg.StateJSON + ".flock")
	if err != nil {
		return out, err
	}
	if !held {
		lg.Printf("skip: another instance holds %s.flock", cfg.StateJSON)
		out.SkippedLock = true
		return out, nil
	}
	defer unlock()

	store, err := Open(cfg.StateDB)
	if err != nil {
		return out, err
	}
	defer store.Close()

	if _, err := store.MigrateFromJSON(ctx, cfg.StateJSON); err != nil {
		// Missing JSON is OK only when DB already has rows / migrated flag; MigrateFromJSON fails closed on missing.
		if !os.IsNotExist(err) && !strings.Contains(err.Error(), "migrate json missing") {
			return out, fmt.Errorf("migrate: %w", err)
		}
		// Ensure empty JSON exists so export merge has a path.
		if _, stErr := os.Stat(cfg.StateJSON); os.IsNotExist(stErr) {
			_ = os.WriteFile(cfg.StateJSON, []byte(`{"workers":{},"meta":{"last_force_unix":0}}`), 0o600)
			if _, err2 := store.MigrateFromJSON(ctx, cfg.StateJSON); err2 != nil {
				return out, fmt.Errorf("migrate: %w", err2)
			}
		} else {
			return out, fmt.Errorf("migrate: %w", err)
		}
	}
	if err := store.ExportJSONToFile(ctx, cfg.StateJSON); err != nil {
		return out, fmt.Errorf("export-json: %w", err)
	}
	lg.Printf("sqlite ledger: %s", cfg.StateDB)

	client := netutil.HTTPClientNoRedirect(time.Duration(cfg.HTTPTimeoutSec) * time.Second)
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}

	now := time.Now().Unix()
	meta, _ := store.GetMeta(ctx)
	force := cfg.ForceSettleAll
	if !force && cfg.DailyForceIntervalSec > 0 && now-meta.LastForceUnix >= cfg.DailyForceIntervalSec {
		force = true
	}
	out.ForceMode = force
	if force {
		lg.Printf("force-settle mode ON (interval=%ds daily_min=%.8f HMC)", cfg.DailyForceIntervalSec, UnitsToHMC(cfg.DailyMinSettleUnits))
	}

	stats, err := fetchWorkStats(ctx, client, cfg.CoordURL, cfg.CoordAdminToken)
	if err != nil {
		return out, fmt.Errorf("coord stats: %w", err)
	}
	payer, err := fetchWalletAddress(ctx, client, cfg.ChainBase, cfg.AdminToken)
	if err != nil || payer == "" {
		return out, fmt.Errorf("resolve payer wallet: %w", err)
	}

	workers := extractWorkers(stats, cfg.WorkerPayoutMap, lg)
	if len(workers) == 0 {
		lg.Printf("no workers in coordinator stats")
		return out, nil
	}

	payoutsSent := 0
	for wid, w := range workers {
		if len(cfg.Allowlist) > 0 {
			if _, ok := cfg.Allowlist[wid]; !ok {
				continue
			}
		}
		if w.AddressConflict {
			lg.Printf("skip %s: address_conflict — refuse payout", wid)
			continue
		}
		toAddr := strings.TrimSpace(cfg.WorkerPayoutMap[wid])
		if toAddr == "" {
			toAddr = strings.TrimSpace(w.PayoutAddress)
		}
		if toAddr == "" {
			lg.Printf("skip %s: no payout address (signed/map)", wid)
			continue
		}
		if toAddr == payer {
			lg.Printf("skip %s: payout address equals payer (%s)", wid, payer)
			continue
		}

		row, err := store.GetWorker(ctx, wid)
		if err != nil {
			lg.Printf("skip %s: get worker: %v", wid, err)
			continue
		}
		accrued := w.PayoutUnits
		if accrued == 0 && w.PayoutHMC > 0 {
			accrued = HMCToUnits(w.PayoutHMC)
		}
		deltaInfo := ComputeAccrualDelta(accrued, row.SettledUnits, row.SessionAnchorUnits)
		if deltaInfo.NeedAnchor {
			lg.Printf("session-anchor %s: settled_units %d kept; anchor=%d (coordinator counter reset)", wid, row.SettledUnits, deltaInfo.NewAnchorUnits)
			if err := store.SetSessionAnchorHMC(ctx, wid, UnitsToHMC(deltaInfo.NewAnchorUnits), false); err != nil {
				lg.Printf("session-anchor write failed for %s: %v", wid, err)
				continue
			}
			_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
		} else if deltaInfo.ClearAnchor {
			_ = store.SetSessionAnchorHMC(ctx, wid, 0, true)
		}

		if !ShouldSettle(deltaInfo.DeltaUnits, cfg.MinSettleUnits, force, cfg.DailyMinSettleUnits) {
			if force {
				lg.Printf("skip %s: delta %s HMC < force-daily min %s", wid, fmtUnits(deltaInfo.DeltaUnits), fmtUnits(cfg.DailyMinSettleUnits))
			} else {
				lg.Printf("skip %s: delta %s HMC < min %s", wid, fmtUnits(deltaInfo.DeltaUnits), fmtUnits(cfg.MinSettleUnits))
			}
			continue
		}
		if deltaInfo.DeltaUnits == 0 {
			continue
		}

		// Crash recovery: pending with tx_hash.
		pend := parsePending(row.PendingJSON)
		if pend.TxHash != "" {
			if ok, _ := verifyTxIncluded(ctx, client, cfg.ChainBase, pend.TxHash, cfg.TxWaitSec); ok {
				newSettled := row.SettledUnits + pend.DeltaUnits
				if pend.DeltaUnits == 0 {
					newSettled = row.SettledUnits + HMCToUnits(pend.DeltaHMC)
				}
				clear := !deltaInfo.Rebase
				anchor := accrued
				if clear {
					anchor = 0
				}
				if err := store.ApplySettleUnits(ctx, wid, firstNonEmpty(pend.PayoutAddress, toAddr), pend.TxHash, newSettled, anchor, clear); err != nil {
					lg.Printf("recover apply %s: %v", wid, err)
					continue
				}
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("recovered pending %s tx=%s settled_units=%d", wid, pend.TxHash, newSettled)
				out.SettledAny = true
				continue
			}
			lg.Printf("skip %s: pending_settle tx=%s not confirmed — not re-sending", wid, pend.TxHash)
			if cfg.ClearPending {
				_ = store.ClearPending(ctx, wid)
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("CLEAR_PENDING_HMC cleared pending for %s", wid)
			}
			continue
		}
		if row.PendingJSON != "" && pend.TxHash == "" {
			lg.Printf("skip %s: pending_settle without tx_hash — not re-sending", wid)
			if cfg.ClearPending {
				_ = store.ClearPending(ctx, wid)
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("CLEAR_PENDING_HMC cleared empty pending for %s", wid)
			}
			continue
		}

		bal, err := fetchBalanceUnits(ctx, client, cfg.ChainBase, payer)
		if err != nil {
			lg.Printf("ERROR settle %s: balance: %v", wid, err)
			continue
		}
		payUnits := CapChunk(deltaInfo.DeltaUnits, bal, cfg.TreasuryReserveUnits, cfg.MaxChunkUnits)
		if payUnits == 0 {
			lg.Printf("ERROR settle %s: treasury balance %s HMC too low (need topup)", wid, fmtUnits(bal))
			continue
		}
		if payUnits < deltaInfo.DeltaUnits {
			lg.Printf("chunk %s: treasury=%s HMC — paying %s of %s HMC now", wid, fmtUnits(bal), fmtUnits(payUnits), fmtUnits(deltaInfo.DeltaUnits))
		}

		if cfg.Sequential && payoutsSent > 0 {
			_ = waitPoolClear(ctx, client, cfg.ChainBase, cfg.TxWaitSec)
			if cfg.PayoutPauseSec > 0 {
				select {
				case <-ctx.Done():
					return out, ctx.Err()
				case <-time.After(time.Duration(cfg.PayoutPauseSec) * time.Second):
				}
			}
		}

		nonce, err := fetchNonce(ctx, client, cfg.ChainBase, payer)
		if err != nil {
			lg.Printf("ERROR settle %s: nonce: %v", wid, err)
			continue
		}
		nonceBefore := nonce
		if _, err := store.SetPendingUnits(ctx, wid, toAddr, payUnits, int64(nonce)); err != nil {
			lg.Printf("ERROR pending intent %s: %v", wid, err)
			continue
		}
		if err := store.ExportJSONToFile(ctx, cfg.StateJSON); err != nil {
			return out, fmt.Errorf("export-json after pending: %w", err)
		}

		ts := time.Now().Unix()
		resp, code, sendErr := sendTransfer(ctx, client, cfg, payer, toAddr, payUnits, nonce, ts)
		attempt := 1
		for !txResponseOK(resp) && attempt < cfg.NonceRetries {
			if code != "pending_nonce_conflict" && code != "invalid_nonce" {
				if code == "insufficient_balance" {
					lg.Printf("ERROR settle %s: treasury insufficient_balance", wid)
				}
				break
			}
			_ = waitPoolClear(ctx, client, cfg.ChainBase, cfg.TxWaitSec)
			sleepSec := cfg.PayoutPauseSec + attempt*2
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(time.Duration(sleepSec) * time.Second):
			}
			nonce, _ = fetchNonce(ctx, client, cfg.ChainBase, payer)
			resp, code, sendErr = sendTransfer(ctx, client, cfg, payer, toAddr, payUnits, nonce, ts)
			attempt++
		}
		if !txResponseOK(resp) {
			nonceAfter, _ := fetchNonce(ctx, client, cfg.ChainBase, payer)
			if nonceAfter > nonceBefore {
				lg.Printf("ALERT ambiguous_pending %s: nonce %d->%d without verifiable tx_hash — keeping pending (manual CLEAR_PENDING_HMC if rejected)", wid, nonceBefore, nonceAfter)
				_ = store.AppendLedger(ctx, "ambiguous_pending", wid, payUnits, 0, 0, 0, 0, 0,
					fmt.Sprintf("nonce %d->%d", nonceBefore, nonceAfter))
			} else {
				lg.Printf("ERROR settle %s -> %s: %v %s", wid, toAddr, sendErr, string(mustJSON(resp)))
				_ = store.ClearPending(ctx, wid)
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
			}
			continue
		}
		txHash := strings.TrimSpace(asString(resp["tx_hash"]))
		if txHash != "" {
			_ = store.AttachTxHash(ctx, wid, txHash)
			_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
		}
		ok, _ := verifyTxIncluded(ctx, client, cfg.ChainBase, txHash, cfg.TxWaitSec)
		if txHash == "" || !ok {
			lg.Printf("ERROR settle %s: tx_hash missing or not confirmed (%s) — pending retained", wid, txHash)
			continue
		}

		newSettled := row.SettledUnits + payUnits
		clear := !deltaInfo.Rebase
		// Partial chunk under rebase: advance anchor by paid amount only so unpaid
		// remainder stays payable next run (never jump anchor to full accrued).
		anchorU := deltaInfo.BaselineUnits + payUnits
		if clear {
			anchorU = 0
		}
		if err := store.ApplySettleUnits(ctx, wid, toAddr, txHash, newSettled, anchorU, clear); err != nil {
			lg.Printf("ERROR state update %s: %v", wid, err)
			continue
		}
		// Ledger row is written inside ApplySettleUnits (kind=payout_applied); do not duplicate.
		if err := store.ExportJSONToFile(ctx, cfg.StateJSON); err != nil {
			return out, fmt.Errorf("export-json after settle: %w", err)
		}
		lg.Printf("settled %s -> %s delta=%s HMC (%d units) tx=%s", wid, toAddr, fmtUnits(payUnits), payUnits, txHash)
		out.SettledAny = true
		payoutsSent++
	}
	out.PayoutsSent = payoutsSent

	if out.SettledAny {
		lg.Printf("done: payouts submitted")
	} else {
		lg.Printf("done: nothing to settle")
	}
	if force && out.SettledAny {
		_ = store.SetLastForceUnix(ctx, now)
		_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
	} else if force {
		lg.Printf("force window not advanced (no successful settle this run)")
	}

	if cfg.PublishScript != "" {
		if st, err := os.Stat(cfg.PublishScript); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			cmd := exec.CommandContext(ctx, "bash", cfg.PublishScript)
			cmd.Env = append(os.Environ(), "STATE_FILE="+cfg.StateJSON)
			_ = cmd.Run()
		}
	}
	return out, nil
}

// --- helpers ---

type workerAccrual struct {
	PayoutHMC       float64
	PayoutUnits     uint64
	PayoutAddress   string
	AddressConflict bool
}

type pendingSettle struct {
	DeltaHMC      float64 `json:"delta_hmc"`
	DeltaUnits    uint64  `json:"delta_units"`
	PayoutAddress string  `json:"payout_address"`
	TxHash        string  `json:"tx_hash"`
	Nonce         int64   `json:"nonce"`
}

func parsePending(raw string) pendingSettle {
	var p pendingSettle
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

func extractWorkers(stats map[string]any, payoutMap map[string]string, lg *log.Logger) map[string]workerAccrual {
	out := map[string]workerAccrual{}
	raw, _ := stats["workers"].(map[string]any)
	for wid, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out[wid] = workerAccrual{
			PayoutHMC:       anyFloat(m["payout_hmc"]),
			PayoutUnits:     anyUint64(m["payout_units"]),
			PayoutAddress:   mapString(m, "payout_address"),
			AddressConflict: anyBool(m["address_conflict"]),
		}
	}
	if len(out) > 0 {
		return out
	}
	wcount := int(anyFloat(stats["workers_count"]))
	tp := anyFloat(stats["total_payout_hmc"])
	tpu := anyUint64(stats["total_payout_units"])
	if tpu == 0 && tp > 0 {
		tpu = HMCToUnits(tp)
	}
	if wcount > 0 && tpu > 0 {
		if len(payoutMap) == 1 {
			for wid := range payoutMap {
				out[wid] = workerAccrual{PayoutHMC: tp, PayoutUnits: tpu}
				lg.Printf("coordinator omitted workers{} — synthetic row for %s using pool total_payout_units=%d", wid, tpu)
			}
		} else if len(payoutMap) > 1 {
			lg.Printf("ERROR: workers{} empty but WORKER_PAYOUT_MAP has %d keys; cannot split pool total safely", len(payoutMap))
		}
	}
	return out
}

func fetchWorkStats(ctx context.Context, client *http.Client, base, token string) (map[string]any, error) {
	url := strings.TrimRight(base, "/") + "/api/work/stats?details=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hackme-Admin-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func fetchWalletAddress(ctx context.Context, client *http.Client, base, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/wallet", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Hackme-Admin-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	return strings.TrimSpace(asString(m["address"])), nil
}

func fetchBalanceUnits(ctx context.Context, client *http.Client, base, addr string) (uint64, error) {
	resp, err := client.Get(strings.TrimRight(base, "/") + "/api/address/" + addr)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return 0, err
	}
	return anyUint64(m["balance_units"]), nil
}

func fetchNonce(ctx context.Context, client *http.Client, base, addr string) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/address/"+addr, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return 0, err
	}
	return anyUint64(m["next_nonce"]), nil
}

func sendTransfer(ctx context.Context, client *http.Client, cfg Config, from, to string, amount, nonce uint64, ts int64) (map[string]any, string, error) {
	fee := cfg.FeeUnits
	if fee == 0 {
		fee = chain.DefaultTransferMinFee
	}
	body, _ := json.Marshal(map[string]any{
		"tx_type":        "transfer_v1",
		"from":           from,
		"to":             to,
		"amount_units":   amount,
		"fee_units":      fee,
		"nonce":          nonce,
		"timestamp_unix": ts,
		"memo":           "worker_settlement",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.ChainBase, "/")+"/api/tx/send", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", cfg.AdminToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m, strings.TrimSpace(asString(m["code"])), nil
}

func txResponseOK(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	txHash := strings.TrimSpace(asString(resp["tx_hash"]))
	if txHash == "" || txHash == "nonce-advanced" || txHash == "ok" {
		return false
	}
	if anyBool(resp["ok"]) {
		return true
	}
	st := strings.TrimSpace(asString(resp["status"]))
	return st == "pending" || st == "accepted" || st == "included"
}

func verifyTxIncluded(ctx context.Context, client *http.Client, base, txHash string, waitSec int) (bool, error) {
	txHash = strings.TrimSpace(txHash)
	if txHash == "" || txHash == "nonce-advanced" || txHash == "ok" {
		return false, nil
	}
	if waitSec <= 0 {
		waitSec = 90
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/tx/"+txHash, nil)
		if err != nil {
			return false, err
		}
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			st := strings.TrimSpace(asString(m["status"]))
			if st == "" {
				if tx, ok := m["tx"].(map[string]any); ok {
					st = strings.TrimSpace(asString(tx["status"]))
				}
			}
			switch st {
			case "included":
				return true, nil
			case "rejected":
				return false, nil
			}
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return false, nil
}

func waitPoolClear(ctx context.Context, client *http.Client, base string, waitSec int) error {
	if waitSec <= 0 {
		waitSec = 90
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(strings.TrimRight(base, "/") + "/api/tx/pool")
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			txs, _ := m["txs"].([]any)
			if len(txs) == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("tx pool still pending after %ds", waitSec)
}

func tryFlock(path string) (unlock func(), held bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}

func (c *Config) checkHostname() error {
	want := strings.TrimSpace(c.ExpectedHostname)
	if want == "" {
		return nil
	}
	hn, _ := os.Hostname()
	short := hn
	if i := strings.IndexByte(hn, '.'); i > 0 {
		short = hn[:i]
	}
	if short != want && hn != want {
		return fmt.Errorf("refuse: hostname=%s/%s != SETTLE_EXPECTED_HOSTNAME=%s", short, hn, want)
	}
	return nil
}

func (c *Config) loadTokensFromSecrets() error {
	root := findRepoRoot()
	if c.AdminToken == "" {
		c.AdminToken = readSecret(filepath.Join(root, ".secrets/hackme_admin_token"))
	}
	if c.CoordAdminToken == "" {
		c.CoordAdminToken = readSecret(filepath.Join(root, ".secrets/hackme_coordinator_admin_token"))
	}
	return nil
}

func findRepoRoot() string {
	wd, _ := os.Getwd()
	return wd
}

func readSecret(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\r", ""))
}

func parsePayoutMap(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "=") {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func parseAllowlist(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = struct{}{}
		}
	}
	return out
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return def
	}
	return f
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func envBool(k string) bool {
	v := strings.TrimSpace(os.Getenv(k))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func anyUint64(v any) uint64 {
	switch t := v.(type) {
	case float64:
		if t < 0 {
			return 0
		}
		return uint64(t + 0.5)
	case int64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case int:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case json.Number:
		f, _ := t.Float64()
		if f < 0 {
			return 0
		}
		return uint64(f + 0.5)
	case string:
		var u uint64
		fmt.Sscanf(t, "%d", &u)
		return u
	default:
		return 0
	}
}

func anyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	default:
		return false
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func mapString(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func fmtUnits(u uint64) string {
	return fmt.Sprintf("%.8f", UnitsToHMC(u))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
