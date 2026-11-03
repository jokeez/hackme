package workersettle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
)

// RunSUP settles coordinator SUP accrual by SUP transfer from the settlement wallet
// (Economics v2: blocks mint SUP to that wallet; admin mint is retired). It runs only once the
// chain reports admin_mint_retired — before v2, settle_worker_sup.sh mints as before.
//
// Same safety contract as the HMC Run: durable intent before the send, tx_hash stamped before
// apply, the watermark moves only after the chain shows the tx included, and an advanced nonce
// without a verifiable tx keeps the intent (never re-send, never double-pay). It shares the HMC
// settler flock so the two never interleave on the same ledger.
func RunSUP(ctx context.Context, cfg Config) (Result, error) {
	cfg.ResolveStateDB()
	lg := cfg.logger()
	var out Result
	if err := cfg.checkHostname(); err != nil {
		return out, err
	}
	if err := cfg.loadTokensFromSecrets(); err != nil {
		return out, err
	}
	if strings.TrimSpace(cfg.AdminToken) == "" || strings.TrimSpace(cfg.CoordAdminToken) == "" {
		return out, fmt.Errorf("ADMIN_TOKEN and COORD_ADMIN_TOKEN are required")
	}
	if strings.TrimSpace(cfg.StateDB) == "" {
		return out, fmt.Errorf("STATE_DB required (SQLite ledger)")
	}
	minUnits := HMCToUnits(envFloat("MIN_SETTLE_SUP", 0.01))
	reserveUnits := HMCToUnits(envFloat("SUP_TREASURY_RESERVE", 0.001))
	if reserveUnits < chain.DefaultSUPTransferMinFee {
		reserveUnits = chain.DefaultSUPTransferMinFee // the payer also pays the SUP fee
	}
	maxChunk := HMCToUnits(envFloat("SUP_SETTLE_MAX_CHUNK", 5))

	if err := os.MkdirAll(filepath.Dir(cfg.StateJSON), 0o700); err != nil && filepath.Dir(cfg.StateJSON) != "." {
		return out, err
	}
	unlock, held, err := tryFlock(cfg.StateJSON + ".flock")
	if err != nil {
		return out, err
	}
	if !held {
		lg.Printf("sup: skip, another settler holds %s.flock", cfg.StateJSON)
		out.SkippedLock = true
		return out, nil
	}
	defer unlock()

	client := netutil.HTTPClientNoRedirect(time.Duration(cfg.HTTPTimeoutSec) * time.Second)
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second}
	}
	retired, err := fetchSUPMintRetired(ctx, client, cfg.ChainBase)
	if err != nil {
		return out, fmt.Errorf("sup economics: %w", err)
	}
	if !retired {
		lg.Printf("sup: admin mint not retired (pre Economics v2) — settle_worker_sup.sh mints; nothing to do")
		return out, nil
	}

	store, err := Open(cfg.StateDB)
	if err != nil {
		return out, err
	}
	defer store.Close()

	stats, err := fetchWorkStats(ctx, client, cfg.CoordURL, cfg.CoordAdminToken)
	if err != nil {
		return out, fmt.Errorf("coord stats: %w", err)
	}
	payer, err := fetchWalletAddress(ctx, client, cfg.ChainBase, cfg.AdminToken)
	if err != nil || payer == "" {
		return out, fmt.Errorf("resolve payer wallet: %w", err)
	}
	raw, _ := stats["workers"].(map[string]any)
	sent := 0
	for wid, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if len(cfg.Allowlist) > 0 {
			if _, ok := cfg.Allowlist[wid]; !ok {
				continue
			}
		}
		if anyBool(m["address_conflict"]) {
			lg.Printf("sup: skip %s: address_conflict", wid)
			continue
		}
		toAddr := firstNonEmpty(cfg.WorkerPayoutMap[wid], mapString(m, "payout_address"))
		if toAddr == "" || toAddr == payer {
			continue
		}
		accrued := HMCToUnits(anyFloat(m["payout_sup"]))
		row, err := store.GetWorker(ctx, wid)
		if err != nil {
			lg.Printf("sup: skip %s: %v", wid, err)
			continue
		}
		d := ComputeAccrualDelta(accrued, row.SettledSUPUnits, row.SessionAnchorSUPU)
		if d.NeedAnchor {
			if err := store.SetSessionAnchorSUP(ctx, wid, UnitsToHMC(d.NewAnchorUnits), false); err != nil {
				lg.Printf("sup: anchor %s: %v", wid, err)
				continue
			}
		} else if d.ClearAnchor {
			_ = store.SetSessionAnchorSUP(ctx, wid, 0, true)
		}

		// Crash recovery / ambiguity: an intent exists — confirm it, never re-send blindly.
		if pendRaw := strings.TrimSpace(row.PendingSUPJSON); pendRaw != "" {
			var pend struct {
				TxHash     string `json:"tx_hash"`
				DeltaUnits uint64 `json:"delta_units"`
				Payout     string `json:"payout_address"`
				Nonce      uint64 `json:"nonce"`
			}
			_ = json.Unmarshal([]byte(pendRaw), &pend)
			if pend.TxHash == "" && envBool("CLEAR_PENDING_SUP") {
				// Manual clear of an intent that never got a tx_hash: only while the payer nonce
				// has not moved past it (otherwise the send may have landed — ambiguous).
				if _, nNow, nerr := fetchSUPState(ctx, client, cfg.ChainBase, payer); nerr == nil && nNow <= pend.Nonce {
					_ = store.ClearPendingSUP(ctx, wid)
					lg.Printf("sup: CLEAR_PENDING_SUP cleared %s (nonce %d not consumed)", wid, pend.Nonce)
				} else {
					lg.Printf("sup: CLEAR_PENDING_SUP refused for %s: nonce advanced past %d", wid, pend.Nonce)
				}
				continue
			}
			if pend.TxHash != "" {
				okIncl, rejected, _ := verifySUPTxIncluded(ctx, client, cfg.ChainBase, pend.TxHash, cfg.TxWaitSec)
				if rejected {
					// The chain refused this exact tx: it can never apply, so the intent is safe to drop.
					_ = store.ClearPendingSUP(ctx, wid)
					_ = store.AppendLedger(ctx, "sup_pending_rejected", wid, pend.DeltaUnits, 0, 0, 0, 0, 0, pend.TxHash)
					lg.Printf("sup: %s tx=%s rejected by chain — intent cleared, retried next run", wid, pend.TxHash)
					continue
				}
				if okIncl {
					anchor := d.BaselineUnits + pend.DeltaUnits
					if err := store.ApplySettleSUPUnits(ctx, wid, firstNonEmpty(pend.Payout, toAddr), pend.TxHash,
						row.SettledSUPUnits+pend.DeltaUnits, anchor, !d.Rebase); err != nil {
						lg.Printf("sup: recover apply %s: %v", wid, err)
						continue
					}
					lg.Printf("sup: recovered %s tx=%s +%d units", wid, pend.TxHash, pend.DeltaUnits)
					out.SettledAny = true
					continue
				}
			}
			lg.Printf("sup: skip %s: pending SUP intent not confirmed (tx=%q) — not re-sending", wid, pend.TxHash)
			continue
		}
		if !ShouldSettle(d.DeltaUnits, minUnits, false, 0) || d.DeltaUnits == 0 {
			continue
		}
		bal, nonce, err := fetchSUPState(ctx, client, cfg.ChainBase, payer)
		if err != nil {
			lg.Printf("sup: ERROR %s: payer state: %v", wid, err)
			continue
		}
		pay := CapChunk(d.DeltaUnits, bal, reserveUnits+chain.DefaultSUPTransferMinFee, maxChunk)
		if pay == 0 {
			lg.Printf("sup: ERROR %s: settlement wallet SUP %s too low", wid, fmtUnits(bal))
			continue
		}
		if sent > 0 && cfg.Sequential {
			_ = waitSUPPoolClear(ctx, client, cfg.ChainBase, cfg.TxWaitSec)
		}
		if err := store.SetPendingSUPUnits(ctx, wid, toAddr, pay, int64(nonce)); err != nil {
			lg.Printf("sup: ERROR intent %s: %v", wid, err)
			continue
		}
		resp, code, sendErr := sendSUPTransfer(ctx, client, cfg, payer, toAddr, pay, nonce)
		if !txResponseOK(resp) {
			_, nonceAfter, _ := fetchSUPState(ctx, client, cfg.ChainBase, payer)
			if nonceAfter > nonce {
				lg.Printf("sup: ALERT ambiguous %s: nonce %d->%d without tx_hash — intent kept", wid, nonce, nonceAfter)
				_ = store.AppendLedger(ctx, "sup_ambiguous_pending", wid, pay, 0, 0, 0, 0, 0, fmt.Sprintf("nonce %d->%d code=%s", nonce, nonceAfter, code))
			} else {
				lg.Printf("sup: ERROR %s -> %s: %v %s", wid, toAddr, sendErr, code)
				_ = store.ClearPendingSUP(ctx, wid)
			}
			continue
		}
		txHash := strings.TrimSpace(asString(resp["tx_hash"]))
		if txHash == "" {
			lg.Printf("sup: ALERT %s: OK without tx_hash — intent kept", wid)
			continue
		}
		if err := store.AttachSUPTxHash(ctx, wid, txHash); err != nil {
			lg.Printf("sup: ALERT %s: cannot stamp tx_hash=%s (%v) — intent kept", wid, txHash, err)
			continue
		}
		if okIncl, _, _ := verifySUPTxIncluded(ctx, client, cfg.ChainBase, txHash, cfg.TxWaitSec); !okIncl {
			lg.Printf("sup: %s tx=%s not confirmed yet — next run recovers it", wid, txHash)
			continue
		}
		anchor := d.BaselineUnits + pay
		if err := store.ApplySettleSUPUnits(ctx, wid, toAddr, txHash, row.SettledSUPUnits+pay, anchor, !d.Rebase); err != nil {
			lg.Printf("sup: ERROR apply %s: %v", wid, err)
			continue
		}
		lg.Printf("sup: settled %s -> %s %s SUP tx=%s", wid, toAddr, fmtUnits(pay), txHash)
		out.SettledAny = true
		sent++
	}
	out.PayoutsSent = sent
	_ = store.ExportJSONToFile(ctx, cfg.StateJSON)
	return out, nil
}

func fetchSUPMintRetired(ctx context.Context, client *http.Client, base string) (bool, error) {
	m, err := getJSON(ctx, client, strings.TrimRight(base, "/")+"/api/sup/economics")
	if err != nil {
		return false, err
	}
	ec, _ := m["economics"].(map[string]any)
	return anyBool(ec["admin_mint_retired"]), nil
}

// fetchSUPState returns the payer's SUP balance and next SUP nonce (absent ⇒ 0).
func fetchSUPState(ctx context.Context, client *http.Client, base, addr string) (uint64, uint64, error) {
	m, err := getJSON(ctx, client, strings.TrimRight(base, "/")+"/api/address/"+addr)
	if err != nil {
		return 0, 0, err
	}
	return anyUint64(m["balance_sup_units"]), anyUint64(m["sup_next_nonce"]), nil
}

func sendSUPTransfer(ctx context.Context, client *http.Client, cfg Config, from, to string, amount, nonce uint64) (map[string]any, string, error) {
	body, _ := json.Marshal(map[string]any{
		"tx_type": "transfer_sup_v1", "from": from, "to": to, "amount_units": amount,
		"fee_units": chain.DefaultSUPTransferMinFee, "nonce": nonce,
		"timestamp_unix": time.Now().Unix(), "memo": "worker_sup_settlement",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.ChainBase, "/")+"/api/sup/tx/send", bytes.NewReader(body))
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
		return nil, "", fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return m, asString(m["code"]), nil
}

// verifySUPTxIncluded waits for a definitive status: included, or rejected (never applies).
func verifySUPTxIncluded(ctx context.Context, client *http.Client, base, txHash string, waitSec int) (included, rejected bool, err error) {
	if waitSec <= 0 {
		waitSec = 90
	}
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		m, gerr := getJSON(ctx, client, strings.TrimRight(base, "/")+"/api/sup/tx/"+strings.TrimSpace(txHash))
		if gerr == nil {
			switch strings.TrimSpace(asString(m["status"])) {
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

func waitSUPPoolClear(ctx context.Context, client *http.Client, base string, waitSec int) error {
	deadline := time.Now().Add(time.Duration(waitSec) * time.Second)
	for time.Now().Before(deadline) {
		if m, err := getJSON(ctx, client, strings.TrimRight(base, "/")+"/api/sup/tx/pool"); err == nil {
			if txs, _ := m["txs"].([]any); len(txs) == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("sup tx pool still pending after %ds", waitSec)
}

func getJSON(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
