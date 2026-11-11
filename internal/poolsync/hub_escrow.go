package poolsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
)

// PrepaidAlgebraicMintAllowed is the documented break-glass for minting/accepting
// hubv1 tickets without a live GetFuzzEscrow row on the linked hub node.
// Production D1 should leave this unset and open escrow on the command-node ledger.
func PrepaidAlgebraicMintAllowed() bool {
	return envTruthy("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT")
}

// HubEscrowProbeURL returns the command-node / Orders base used for escrow RPC.
func HubEscrowProbeURL(fallback string) string {
	fallback = strings.TrimRight(strings.TrimSpace(fallback), "/")
	if fallback != "" {
		return fallback
	}
	for _, k := range []string{
		"HACKME_COORDINATOR_ORDERS_URL",
		"HACKME_CANONICAL_CHAIN_URL",
		"HACKME_PUBLIC_AUTHORITY_BASE",
	} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(k)), "/"); v != "" {
			return v
		}
	}
	return ""
}

// FetchHubFuzzEscrow loads GET /api/fuzz/campaigns/{id}/escrow from the hub node.
func FetchHubFuzzEscrow(ctx context.Context, baseURL, adminToken, campaignID string) (*chain.FuzzEscrowRow, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	campaignID = strings.TrimSpace(campaignID)
	if baseURL == "" {
		return nil, fmt.Errorf("hub escrow: orders/hub URL not configured")
	}
	if campaignID == "" {
		return nil, fmt.Errorf("hub escrow: campaign_id required")
	}
	u, err := url.Parse(baseURL + "/api/fuzz/campaigns/" + url.PathEscape(campaignID) + "/escrow")
	if err != nil {
		return nil, fmt.Errorf("hub escrow: bad url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if tok := strings.TrimSpace(adminToken); tok != "" {
		req.Header.Set("X-Hackme-Admin-Token", tok)
	}
	cl := netutil.HTTPClientNoRedirect(4 * time.Second)
	res, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hub escrow: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusNotFound {
		return nil, chain.ErrFuzzEscrowNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub escrow: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var wrap struct {
		OK     bool                 `json:"ok"`
		Escrow *chain.FuzzEscrowRow `json:"escrow"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("hub escrow: decode: %w", err)
	}
	if wrap.Escrow == nil {
		return nil, chain.ErrFuzzEscrowNotFound
	}
	return wrap.Escrow, nil
}

// MatchOpenHubEscrowForPrepaid checks an open hub escrow covers the claimed prepaid budget.
func MatchOpenHubEscrowForPrepaid(row *chain.FuzzEscrowRow, campaignID string, budgetHMC float64, budgetRuns int, wantFP string) error {
	if row == nil {
		return chain.ErrFuzzEscrowNotFound
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return fmt.Errorf("hub escrow: campaign_id required")
	}
	if id := strings.TrimSpace(row.CampaignID); id != "" && !strings.EqualFold(id, campaignID) {
		return fmt.Errorf("hub escrow: campaign_id mismatch")
	}
	st := strings.ToLower(strings.TrimSpace(row.Status))
	if st != "open" {
		return fmt.Errorf("hub escrow: status %q not open", row.Status)
	}
	if budgetHMC <= 0 {
		return fmt.Errorf("hub escrow: budget_hmc required")
	}
	if row.BudgetHMC+1e-9 < budgetHMC {
		return fmt.Errorf("hub escrow: budget_hmc %.4f below claim %.4f", row.BudgetHMC, budgetHMC)
	}
	if budgetRuns > 0 && row.BudgetRuns < budgetRuns {
		return fmt.Errorf("hub escrow: budget_runs %d below claim %d", row.BudgetRuns, budgetRuns)
	}
	fp := EscrowPrepaidFingerprintParts(campaignID, budgetHMC, budgetRuns, "open")
	wantFP = strings.ToLower(strings.TrimSpace(wantFP))
	if wantFP == "" || !strings.EqualFold(fp, wantFP) {
		return fmt.Errorf("hub escrow: escrow_fingerprint mismatch for open claim")
	}
	return nil
}

// VerifyHubPrepaidEscrowLive fetches hub escrow and matches the prepaid claim.
// When algebraic break-glass is on, skips the RPC (returns nil).
func VerifyHubPrepaidEscrowLive(ctx context.Context, baseURL, adminToken, campaignID string, budgetHMC float64, budgetRuns int, wantFP string) error {
	if PrepaidAlgebraicMintAllowed() {
		return nil
	}
	row, err := FetchHubFuzzEscrow(ctx, baseURL, adminToken, campaignID)
	if err != nil {
		return err
	}
	return MatchOpenHubEscrowForPrepaid(row, campaignID, budgetHMC, budgetRuns, wantFP)
}
