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

// A payout intent can be left "pending without tx_hash" when the send's response was lost or
// the nonce moved during a conflict retry (ambiguous_pending). The settler never re-sends such
// an intent blindly (double pay). resolveStrandedPending decides it from the chain instead:
//
//   - a transfer payer→payout of exactly the intent amount at a nonce the intent could have
//     taken ([intent nonce, next nonce)) → the payout landed: attach its hash, and the normal
//     crash-recovery path verifies inclusion and applies it;
//   - every nonce in that range is taken by another included transfer and the mempool holds no
//     matching transfer → it was never sent: clear the intent, the next cycle pays it;
//   - anything else (an empty or rejected-only slot, a matching transfer still pending, lookup
//     errors, a range wider than strandedMaxScan) → keep the intent for an operator.

type strandedOutcome int

const (
	strandedKeep strandedOutcome = iota
	strandedPaid
	strandedUnsent
)

// strandedMaxScan bounds the nonce range scanned per intent (one lookup per nonce).
const strandedMaxScan = 256

type strandedResult struct {
	Outcome strandedOutcome
	TxHash  string
	Nonce   uint64
	Reason  string
}

type byNonceTx struct {
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"`
	To          string `json:"to"`
	AmountUnits uint64 `json:"amount_units"`
}

func fetchTxsByNonce(ctx context.Context, client *http.Client, base, from string, nonce uint64) ([]byNonceTx, error) {
	q := url.Values{"from": {from}, "nonce": {fmt.Sprint(nonce)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/tx/by-nonce?"+q.Encode(), nil)
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
		return nil, fmt.Errorf("by-nonce: http %d", resp.StatusCode)
	}
	var m struct {
		Txs []byNonceTx `json:"txs"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m.Txs, nil
}

// findInflightTransfer returns the hash of a non-rejected transfer payer→to of amount at nonce
// ("" when the node holds none; err when the lookup itself failed).
func findInflightTransfer(ctx context.Context, client *http.Client, base, payer, to string, amount, nonce uint64) (string, error) {
	txs, err := fetchTxsByNonce(ctx, client, base, payer, nonce)
	if err != nil {
		return "", err
	}
	for _, t := range txs {
		if t.To == to && t.AmountUnits == amount && t.Status != "rejected" && strings.TrimSpace(t.TxHash) != "" {
			return t.TxHash, nil
		}
	}
	return "", nil
}

// poolHasTransfer reports whether the mempool holds from→to of amount (err when unknown).
func poolHasTransfer(ctx context.Context, client *http.Client, base, from, to string, amount uint64) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/tx/pool", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("pool: http %d", resp.StatusCode)
	}
	var m struct {
		Txs []struct {
			From        string `json:"from"`
			To          string `json:"to"`
			AmountUnits uint64 `json:"amount_units"`
		} `json:"txs"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return false, err
	}
	for _, t := range m.Txs {
		if t.From == from && t.To == to && t.AmountUnits == amount {
			return true, nil
		}
	}
	return false, nil
}

func resolveStrandedPending(ctx context.Context, client *http.Client, base, payer, to string, pend pendingSettle) strandedResult {
	amount := pend.DeltaUnits
	if amount == 0 {
		amount = HMCToUnits(pend.DeltaHMC)
	}
	to = strings.TrimSpace(to)
	if pend.Nonce <= 0 || amount == 0 || to == "" {
		return strandedResult{Reason: "intent has no nonce/amount/address"}
	}
	start := uint64(pend.Nonce)
	next, err := fetchNonce(ctx, client, base, payer)
	if err != nil {
		return strandedResult{Reason: "nonce lookup: " + err.Error()}
	}
	if next < start {
		// The chain says the payer never reached the intent's nonce: a stale view (follower
		// cache, restored DB). Clearing now could re-send at a lower nonce — keep for an operator.
		return strandedResult{Reason: fmt.Sprintf("payer next_nonce %d is behind intent nonce %d", next, start)}
	}
	if next > start && next-start > strandedMaxScan {
		return strandedResult{Reason: fmt.Sprintf("nonce range %d..%d too wide", start, next)}
	}
	for n := start; n < next; n++ {
		txs, err := fetchTxsByNonce(ctx, client, base, payer, n)
		if err != nil {
			return strandedResult{Reason: fmt.Sprintf("nonce %d lookup: %v", n, err)}
		}
		taken := false
		for _, t := range txs {
			if t.To == to && t.AmountUnits == amount {
				switch t.Status {
				case "included":
					return strandedResult{Outcome: strandedPaid, TxHash: t.TxHash, Nonce: n}
				case "rejected":
					continue
				default: // still in the mempool: wait for it
					return strandedResult{Reason: fmt.Sprintf("matching tx %s at nonce %d is %s", t.TxHash, n, t.Status), Nonce: n}
				}
			}
			if t.Status == "included" {
				if t.To == to {
					// Same recipient, different amount: may be this worker's earlier intent for an
					// older delta. Treating the slot as "someone else's" would re-pay — keep.
					return strandedResult{Reason: fmt.Sprintf("included transfer %s at nonce %d pays the same address a different amount", t.TxHash, n), Nonce: n}
				}
				taken = true
			}
		}
		if !taken {
			return strandedResult{Reason: fmt.Sprintf("nonce %d has no included transfer", n), Nonce: n}
		}
	}
	inPool, err := poolHasTransfer(ctx, client, base, payer, to, amount)
	if err != nil {
		return strandedResult{Reason: "pool lookup: " + err.Error()}
	}
	if inPool {
		return strandedResult{Reason: "matching transfer pending in mempool"}
	}
	return strandedResult{Outcome: strandedUnsent}
}
