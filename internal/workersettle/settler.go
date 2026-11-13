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
	"strconv"
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
	// ClearPendingForce allows dropping empty-hash pending when the payer nonce
	// may have been consumed (operator-attested). Pending with a tx_hash only
	// clears on explicit chain "rejected" — never via FORCE alone.
	ClearPendingForce bool
	ExpectedHostname  string
	FeeUnits          uint64
	PublishScript     string // optional absolute path; empty skips
	Log               *log.Logger
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
		ClearPendingForce:     envBool("CLEAR_PENDING_HMC_FORCE") || envBool("CLEAR_PENDING_SETTLE_FORCE"),
		ExpectedHostname:      strings.TrimSpace(os.Getenv("SETTLE_EXPECTED_HOSTNAME")),
		FeeUnits:              chain.DefaultTransferMinFee,
		WorkerPayoutMap:       parsePayoutMap(os.Getenv("WORKER_PAYOUT_MAP")),
		Allowlist:             parseAllowlist(firstNonEmpty(os.Getenv("SETTLE_WORKER_ALLOWLIST"), os.Getenv("HACKME_SETTLE_WORKER_ALLOWLIST"))),
	}
}

// ResolveStateDB takes the SQLite ledger from STATE_DB / HACKME_WORKER_SETTLEMENT_DB.
// It never derives one beside STATE_FILE: a derived ledger has its own zero watermarks
// and re-pays accrual the main ledger already settled (2026-10-29..11-12: ~1153 HMC paid
// twice). With no explicit ledger StateDB stays empty and Run/RunSUP refuse.
func (c *Config) ResolveStateDB() {
	if strings.TrimSpace(c.StateDB) != "" {
		return
	}
	c.StateDB = strings.TrimSpace(firstNonEmpty(os.Getenv("STATE_DB"), os.Getenv("HACKME_WORKER_SETTLEMENT_DB")))
}

// checkLedgerPath refuses a relative ledger: the same name from two working
// directories would be two ledgers with two locks.
func (c *Config) checkLedgerPath() error {
	if strings.TrimSpace(c.StateDB) == "" {
		return fmt.Errorf("STATE_DB required: name the one settlement ledger explicitly (STATE_DB or HACKME_WORKER_SETTLEMENT_DB); it is never derived from STATE_FILE")
	}
	if !filepath.IsAbs(c.StateDB) || !filepath.IsAbs(c.StateJSON) {
		return fmt.Errorf("STATE_DB and STATE_FILE must be absolute paths (got %q, %q)", c.StateDB, c.StateJSON)
	}
	return nil
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
	if err := cfg.checkLedgerPath(); err != nil {
		return out, err
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

	client := netutil.HTTPClientNoRedirect(time.Duration(settleHTTPTimeoutSec(cfg)) * time.Second)
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
	unlockPayer, heldPayer, err := lockPayer(ctx, cfg, payer, "hmc")
	if err != nil {
		return out, err
	}
	if !heldPayer {
		lg.Printf("skip: another settler is sending from payer %s (%s)", payer, payerLockPath(cfg, payer, "hmc"))
		out.SkippedLock = true
		return out, nil
	}
	defer unlockPayer()

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

		// Crash recovery runs BEFORE ShouldSettle/delta==0 skips (match SUP settler):
		// a confirmed pending must still apply when current unpaid delta is below min
		// or temporarily zero after a coordinator counter reset.
		pend := parsePending(row.PendingJSON)
		if pend.TxHash != "" {
			ok, rejected, _ := verifyTxIncluded(ctx, client, cfg.ChainBase, pend.TxHash, cfg.TxWaitSec)
			if rejected {
				// Chain refused this exact hash — safe to drop and re-pay next cycle.
				_ = store.ClearPending(ctx, wid)
				_ = store.AppendLedger(ctx, "hmc_pending_rejected", wid, pend.DeltaUnits, 0, 0, 0, 0, 0, pend.TxHash)
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("pending %s tx=%s rejected by chain — intent cleared, retried next run", wid, pend.TxHash)
				continue
			}
			if ok {
				pendDelta := pend.DeltaUnits
				if pendDelta == 0 {
					pendDelta = HMCToUnits(pend.DeltaHMC)
				}
				newSettled := row.SettledUnits + pendDelta
				clear := !deltaInfo.Rebase
				// Same as success-path ApplySettle: under rebase advance anchor by the
				// paid chunk only — never jump to full accrued (lost-pay on remainder).
				anchor := deltaInfo.BaselineUnits + pendDelta
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
				// Refuse FORCE while status is unknown/mempool: clearing + resend = double-pay
				// if the original tx later includes (match SUP rejected-only clear).
				lg.Printf("CLEAR_PENDING_HMC refused for %s: tx_hash=%s not rejected (mempool/unknown)", wid, pend.TxHash)
			}
			continue
		}
		if row.PendingJSON != "" && pend.TxHash == "" {
			// Decide the stranded intent from the chain (payer nonce slots) before any operator step.
			payTo := firstNonEmpty(pend.PayoutAddress, toAddr)
			switch res := resolveStrandedPending(ctx, client, cfg.ChainBase, payer, payTo, pend); res.Outcome {
			case strandedPaid:
				// Never adopt a transfer already credited to another intent (two workers sharing
				// a payout address and amount would otherwise settle one of them for free).
				if used, uerr := store.TxHashKnown(ctx, res.TxHash); uerr != nil || used {
					lg.Printf("stranded pending %s kept: candidate tx=%s already recorded (err=%v)", wid, res.TxHash, uerr)
					continue
				}
				if err := store.AttachTxHash(ctx, wid, res.TxHash); err != nil {
					lg.Printf("resolve %s: attach tx=%s: %v", wid, res.TxHash, err)
					continue
				}
				_ = store.AppendLedger(ctx, "stranded_resolved_paid", wid, pend.DeltaUnits, 0, 0, 0, 0, 0,
					fmt.Sprintf("tx=%s nonce=%d", res.TxHash, res.Nonce))
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("resolved stranded pending %s: paid on chain tx=%s nonce=%d (applied by recovery next cycle)", wid, res.TxHash, res.Nonce)
				continue
			case strandedUnsent:
				if err := store.ClearPending(ctx, wid); err != nil {
					lg.Printf("resolve %s: clear: %v", wid, err)
					continue
				}
				_ = store.AppendLedger(ctx, "stranded_resolved_unsent", wid, pend.DeltaUnits, 0, 0, 0, 0, 0,
					fmt.Sprintf("intent nonce %d: every slot taken by another included transfer", pend.Nonce))
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
				lg.Printf("resolved stranded pending %s: never sent (nonce %d slots taken by other txs) — cleared, re-paid next cycle", wid, pend.Nonce)
				continue
			default:
				lg.Printf("stranded pending %s kept: %s", wid, res.Reason)
				// Resolver kept the intent (mempool match / ambiguous). CLEAR_PENDING
				// here would drop it while an in-flight tx can still include → double-pay
				// on the next cycle. Only strandedUnsent auto-clears above.
				if cfg.ClearPending {
					lg.Printf("CLEAR_PENDING_HMC refused for %s: stranded kept (%s)", wid, res.Reason)
				}
				continue
			}
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
		ambiguousNonce := false
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
			nextNonce, nerr := fetchNonce(ctx, client, cfg.ChainBase, payer)
			if nerr != nil {
				lg.Printf("ERROR settle %s: nonce after conflict: %v", wid, nerr)
				break
			}
			// Critical: if nonce advanced, the first send may already be in
			// mempool/chain. Re-sending the same payUnits with a fresh nonce
			// double-pays the intent. Keep pending; do not retry.
			if nextNonce > nonceBefore {
				lg.Printf("ALERT ambiguous_pending %s: nonce %d->%d during conflict retry — NOT re-sending", wid, nonceBefore, nextNonce)
				_ = store.AppendLedger(ctx, "ambiguous_pending", wid, payUnits, 0, 0, 0, 0, 0,
					fmt.Sprintf("nonce %d->%d conflict-retry", nonceBefore, nextNonce))
				ambiguousNonce = true
				break
			}
			nonce = nextNonce
			resp, code, sendErr = sendTransfer(ctx, client, cfg, payer, toAddr, payUnits, nonce, ts)
			attempt++
		}
		if ambiguousNonce {
			continue
		}
		if !txResponseOK(resp) {
			nonceAfter, _ := fetchNonce(ctx, client, cfg.ChainBase, payer)
			if nonceAfter > nonceBefore {
				lg.Printf("ALERT ambiguous_pending %s: nonce %d->%d without verifiable tx_hash — keeping pending (CLEAR_PENDING_HMC_FORCE only if rejected)", wid, nonceBefore, nonceAfter)
				_ = store.AppendLedger(ctx, "ambiguous_pending", wid, payUnits, 0, 0, 0, 0, 0,
					fmt.Sprintf("nonce %d->%d", nonceBefore, nonceAfter))
			} else if inflight, ferr := findInflightTransfer(ctx, client, cfg.ChainBase, payer, toAddr, payUnits, nonce); ferr != nil || inflight != "" {
				// A lost response does not mean "not sent": the node may hold the tx in its
				// pool. Clearing here and re-sending later (at the next nonce, once it lands)
				// double-pays. Adopt the in-flight hash, or keep the intent when unsure.
				if inflight != "" {
					if aerr := store.AttachTxHash(ctx, wid, inflight); aerr == nil {
						lg.Printf("settle %s: response lost but tx %s is in flight at nonce %d — attached", wid, inflight, nonce)
					}
				} else {
					lg.Printf("ALERT settle %s: send failed and in-flight lookup failed (%v) — keeping pending", wid, ferr)
				}
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
			} else {
				// Empty pool + nonce unmoved still is not proof of "never sent" (race with
				// mempool propagation). Match SUP: keep intent; stranded resolve / rejected
				// clear handles recovery. Clearing here was audit #8 double-pay class.
				lg.Printf("ERROR settle %s -> %s: %v %s — pending retained (no definitive reject)", wid, toAddr, sendErr, string(mustJSON(resp)))
				_ = store.AppendLedger(ctx, "send_failed_keep_pending", wid, payUnits, 0, 0, 0, 0, 0, string(mustJSON(resp)))
				_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
			}
			continue
		}
		txHash := strings.TrimSpace(asString(resp["tx_hash"]))
		if txHash == "" {
			lg.Printf("ALERT settle %s: OK response without tx_hash — pending retained (no clear)", wid)
			_ = store.AppendLedger(ctx, "missing_tx_hash", wid, payUnits, 0, 0, 0, 0, 0, "ok response empty tx_hash")
			continue
		}
		// Must durable-stamp before apply/crash window. Ignoring AttachTxHash left
		// pending without hash → skip forever; CLEAR_PENDING_HMC then resend = double-pay.
		var attachErr error
		for try := 0; try < 8; try++ {
			attachErr = store.AttachTxHash(ctx, wid, txHash)
			if attachErr == nil {
				break
			}
			lg.Printf("settle %s: AttachTxHash try %d/8: %v", wid, try+1, attachErr)
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(time.Duration(50*(try+1)) * time.Millisecond):
			}
		}
		if attachErr != nil {
			lg.Printf("ALERT settle %s: cannot stamp tx_hash=%s (%v) — pending retained, NOT clearing", wid, txHash, attachErr)
			_ = store.AppendLedger(ctx, "attach_tx_hash_failed", wid, payUnits, 0, 0, 0, 0, 0, txHash)
			continue
		}
		_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
		ok, rejected, _ := verifyTxIncluded(ctx, client, cfg.ChainBase, txHash, cfg.TxWaitSec)
		if rejected {
			_ = store.ClearPending(ctx, wid)
			_ = store.AppendLedger(ctx, "hmc_pending_rejected", wid, payUnits, 0, 0, 0, 0, 0, txHash)
			_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
			lg.Printf("ERROR settle %s: tx_hash rejected (%s) — intent cleared, retried next run", wid, txHash)
			continue
		}
		if !ok {
			lg.Printf("ERROR settle %s: tx_hash not confirmed (%s) — pending retained", wid, txHash)
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
		// Legacy solo setups only (SETTLE_SYNTHETIC_SINGLE=1): the pool total covers every miner,
		// so paying it to the single mapped worker overpaid whenever others mined (audit L7).
		if len(payoutMap) == 1 && !envBool("SETTLE_SYNTHETIC_SINGLE") {
			lg.Printf("ERROR: coordinator omitted workers{} — not paying the pool total to one worker (set SETTLE_SYNTHETIC_SINGLE=1 for a solo rig)")
			return out
		}
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
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("address lookup: http %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return 0, err
	}
	v, ok := m["next_nonce"]
	if !ok {
		// A body without next_nonce must not read as "nonce 0" (resolver/clear decisions).
		return 0, fmt.Errorf("address lookup: next_nonce missing")
	}
	return anyUint64(v), nil
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

// verifyTxIncluded reports whether txHash is included. rejected=true only when the
// chain explicitly refused the hash (safe to ClearPending); unknown/mempool stays
// rejected=false so FORCE cannot drop an in-flight pay and re-send (double-pay).
func verifyTxIncluded(ctx context.Context, client *http.Client, base, txHash string, waitSec int) (included, rejected bool, err error) {
	txHash = strings.TrimSpace(txHash)
	if txHash == "" || txHash == "nonce-advanced" || txHash == "ok" {
		return false, false, nil
	}
	if waitSec <= 0 {
		waitSec = 90
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/tx/"+txHash, nil)
		if err != nil {
			return false, false, err
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
				return true, false, nil
			case "rejected":
				return false, true, nil
			}
		}
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return false, false, nil
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

// payerLockPath is one lock per payer address and nonce lane ("hmc" / "sup"), independent of
// STATE_FILE: any settler that signs from this payer (scheduled run, a named-fleet run with
// its own ledger) shares the payer's nonce sequence for that lane, and two of them
// interleaving on it strands intents as ambiguous_pending. The default lives in one
// host-wide directory (/run/lock) so ledgers in different directories still share it;
// HACKME_SETTLE_PAYER_LOCK_DIR overrides the directory.
func payerLockPath(cfg Config, payer, lane string) string {
	dir := strings.TrimSpace(os.Getenv("HACKME_SETTLE_PAYER_LOCK_DIR"))
	if dir == "" {
		if st, err := os.Stat("/run/lock"); err == nil && st.IsDir() {
			dir = "/run/lock"
		} else if strings.TrimSpace(cfg.StateDB) != "" {
			dir = filepath.Dir(cfg.StateDB)
		} else {
			dir = filepath.Dir(cfg.StateJSON)
		}
	}
	return filepath.Join(dir, "hackme-settle-payer-"+sanitizeLockName(payer)+"-"+sanitizeLockName(lane)+".flock")
}

func sanitizeLockName(s string) string {
	b := []byte(strings.TrimSpace(s))
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			b[i] = '_'
		}
	}
	return string(b)
}

// lockPayer waits up to HACKME_SETTLE_PAYER_LOCK_WAIT_SEC (default 90) for the payer lock, so
// a scheduled run and an ad-hoc run serialize instead of racing the nonce. Honors ctx.
func lockPayer(ctx context.Context, cfg Config, payer, lane string) (unlock func(), held bool, err error) {
	path := payerLockPath(cfg, payer, lane)
	wait := 90
	if v := strings.TrimSpace(os.Getenv("HACKME_SETTLE_PAYER_LOCK_WAIT_SEC")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 && n <= 3600 {
			wait = n
		}
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		unlock, held, err = tryFlockShared(path)
		if err != nil || held || !time.Now().Before(deadline) {
			return unlock, held, err
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-t.C:
		}
	}
}

// tryFlockShared is tryFlock for a lock file opened by settlers running as different users
// (systemd unit as hackme, ops runs as root). O_NOFOLLOW: never follow a planted symlink
// (the chmod below would otherwise widen the target's mode). The file holds no data, so
// world-RW only lets another local user hold the lock, never alter a payout.
func tryFlockShared(path string) (unlock func(), held bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil && filepath.Dir(path) != "." {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o666)
	if err != nil {
		return nil, false, fmt.Errorf("payer lock %s: %w", path, err)
	}
	if st, serr := f.Stat(); serr == nil && st.Mode().IsRegular() {
		_ = f.Chmod(0o666)
	} else {
		_ = f.Close()
		return nil, false, fmt.Errorf("payer lock %s: not a regular file", path)
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

// settleHTTPTimeoutSec keeps settle HTTP calls bounded (the payer lock is held across them).
func settleHTTPTimeoutSec(cfg Config) int {
	if cfg.HTTPTimeoutSec > 0 {
		return cfg.HTTPTimeoutSec
	}
	return 30
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
