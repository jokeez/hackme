package poolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OrdersAdminToken mirrors the coordinator's own resolution order (ordersAdminToken in
// cmd/coordinator/orders_pool.go) so a mint request presents the token that endpoint
// actually compares against. CoordinatorAdminToken reads neither of the first two keys,
// and HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN is the one the node deploy syncs — which is
// why the documented "hub admin mint" path had no working client.
func OrdersAdminToken() string {
	for _, k := range []string{
		"HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN",
		"HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN",
		"HACKME_COORDINATOR_ADMIN_TOKEN",
		"HACKME_POOL_COORDINATOR_ADMIN_TOKEN",
	} {
		if t := strings.TrimSpace(os.Getenv(k)); t != "" {
			return t
		}
	}
	return ""
}

// prepaidMintTimeout bounds one mint attempt. The endpoint verifies the escrow against
// the hub ledger before signing, so it is not instant.
const prepaidMintTimeout = 25 * time.Second

// MintHubPrepaidTicket asks the coordinator to sign a prepaid ticket for an open escrow.
//
// This adds no way to forge a ticket and the signing key stays hub-only (PUB-01). The
// endpoint authenticates with the coordinator ADMIN token — which a fleet node holding
// only a publish token does not have — it re-derives the escrow fingerprint itself and
// ignores whatever the caller claims, and it refuses to sign at all unless that escrow is
// live on the hub ledger.
//
// Without this the designed path had no client. Measured on 2026-11-25: 33 campaigns sat
// in status running with an open escrow, 1750.5 HMC locked and zero runs executed, some
// for six days, because the only way to obtain a ticket was for a person to call this
// endpoint by hand — and the token accessors did not even agree, so nobody could have
// scripted it without reading both files.
//
// Returns the ticket and the fingerprint the hub bound it to; publish must send that same
// fingerprint or verification fails.
func MintHubPrepaidTicket(ctx context.Context, coordURL, adminToken, owner, campaignID string, budgetHMC float64, budgetRuns int) (ticket string, escrowFP string, err error) {
	coordURL = strings.TrimRight(strings.TrimSpace(coordURL), "/")
	if coordURL == "" {
		return "", "", fmt.Errorf("coordinator URL not set")
	}
	if strings.TrimSpace(adminToken) == "" {
		return "", "", fmt.Errorf("coordinator admin token not set")
	}
	owner = strings.TrimSpace(owner)
	campaignID = strings.TrimSpace(campaignID)
	if owner == "" || campaignID == "" || budgetHMC <= 0 || budgetRuns <= 0 {
		return "", "", fmt.Errorf("owner_ref, campaign_id, budget_hmc and budget_runs required")
	}
	body, err := json.Marshal(map[string]any{
		"campaign_id": campaignID,
		"owner_ref":   owner,
		"budget_hmc":  budgetHMC,
		"budget_runs": budgetRuns,
	})
	if err != nil {
		return "", "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, prepaidMintTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		coordURL+"/api/fuzz/pool/prepaid-ticket", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
	}()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		// The body is the hub's reason (no escrow, misconfigured secret, bad auth). It
		// never carries the token, and it is the only way an operator learns which.
		return "", "", fmt.Errorf("mint http %d: %s", resp.StatusCode,
			strings.TrimSpace(firstLine(string(payload), 200)))
	}
	var out struct {
		OK                 bool   `json:"ok"`
		PrepaidAttestation string `json:"prepaid_attestation"`
		EscrowFingerprint  string `json:"escrow_fingerprint"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", "", fmt.Errorf("mint reply: %w", err)
	}
	ticket = strings.TrimSpace(out.PrepaidAttestation)
	if !out.OK || ticket == "" {
		return "", "", fmt.Errorf("mint returned no ticket")
	}
	if !IsHubPrepaidTicket(ticket) {
		// A self-MAC here would be refused at publish; say so now instead of later.
		return "", "", fmt.Errorf("mint returned a non-hub ticket")
	}
	return ticket, strings.TrimSpace(out.EscrowFingerprint), nil
}

// firstLine trims a remote message to one short line for an error string.
func firstLine(s string, max int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}
