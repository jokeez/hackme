package workersettle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// resolveStrandedSUPPending mirrors resolveStrandedPending for SUP transfers.
// Empty-hash recovery must not settle on payer nonce movement alone: an unrelated
// SUP payment (or a shared payout address) would false-settle or double-credit
// (valax Medium). Require a matching payer→payout transfer of the intent amount.
func resolveStrandedSUPPending(ctx context.Context, client *http.Client, base, payer, to string, pend pendingSettle) strandedResult {
	amount := pend.DeltaUnits
	if amount == 0 {
		amount = HMCToUnits(pend.DeltaHMC)
	}
	to = strings.TrimSpace(to)
	// Nonce 0 is valid for a fresh SUP account (unlike the conservative HMC gate).
	if pend.Nonce < 0 || amount == 0 || to == "" {
		return strandedResult{Reason: "sup intent has no nonce/amount/address"}
	}
	start := uint64(pend.Nonce)
	_, nNow, err := fetchSUPState(ctx, client, base, payer)
	if err != nil {
		return strandedResult{Reason: "sup nonce lookup: " + err.Error()}
	}
	next := nNow
	if next < start {
		return strandedResult{Reason: fmt.Sprintf("payer sup_next_nonce %d is behind intent nonce %d", next, start)}
	}
	if next > start && next-start > strandedMaxScan {
		return strandedResult{Reason: fmt.Sprintf("sup nonce range %d..%d too wide", start, next)}
	}
	for n := start; n < next; n++ {
		txs, err := fetchSUPTxsByNonce(ctx, client, base, payer, n)
		if err != nil {
			return strandedResult{Reason: fmt.Sprintf("sup nonce %d lookup: %v", n, err)}
		}
		taken := false
		for _, t := range txs {
			if t.To == to && t.AmountUnits == amount {
				switch t.Status {
				case "included":
					return strandedResult{Outcome: strandedPaid, TxHash: t.TxHash, Nonce: n}
				case "rejected":
					continue
				default:
					return strandedResult{Reason: fmt.Sprintf("matching SUP tx %s at nonce %d is %s", t.TxHash, n, t.Status), Nonce: n}
				}
			}
			if t.Status == "included" {
				if t.To == to {
					return strandedResult{Reason: fmt.Sprintf("included SUP %s at nonce %d pays the same address a different amount", t.TxHash, n), Nonce: n}
				}
				taken = true
			}
		}
		if !taken {
			return strandedResult{Reason: fmt.Sprintf("sup nonce %d has no included transfer", n), Nonce: n}
		}
	}
	inflight, err := findSUPInflight(ctx, client, base, payer, to, amount)
	if err != nil {
		return strandedResult{Reason: "sup pool lookup: " + err.Error()}
	}
	if inflight != "" {
		return strandedResult{Reason: "matching SUP transfer pending in mempool", TxHash: inflight}
	}
	return strandedResult{Outcome: strandedUnsent}
}

func fetchSUPTxsByNonce(ctx context.Context, client *http.Client, base, from string, nonce uint64) ([]byNonceTx, error) {
	q := url.Values{"from": {from}, "nonce": {fmt.Sprint(nonce)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/sup/tx/by-nonce?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sup by-nonce: http %d", resp.StatusCode)
	}
	var m struct {
		Txs []byNonceTx `json:"txs"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m.Txs, nil
}
