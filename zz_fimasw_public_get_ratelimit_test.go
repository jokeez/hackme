package main

import (
	"os"
	"strings"
	"testing"
)

// fimasw 2026-11-19: public GETs with fan-out / large SQLite pages must allowRate
// (sibling of global_metrics Medium).
func TestFimaswPublicGETHandlersHaveAllowRate(t *testing.T) {
	cases := []struct {
		file string
		fn   string
		key  string
	}{
		{"reports.go", "func (a *app) handleReportsBlocks(", "reports_blocks:"},
		{"main.go", "func (a *app) handleChain(", "chain_get:"},
		{"main.go", "func (a *app) handleWalletActivity(", "wallet_activity:"},
		{"main.go", "func (a *app) handleWalletEarnings(", "wallet_earnings:"},
		{"main.go", "func (a *app) handleSUPActivity(", "sup_activity:"},
		{"main.go", "func (a *app) handleTransferPool(", "tx_pool:"},
		{"main.go", "func (a *app) handleTransferByHash(", "tx_by_hash:"},
		{"main.go", "func (a *app) handleWorkerStatus(", "worker_status:"},
		{"miner_notices.go", "func (a *app) handleMinerNotices(", "miner_notices:"},
		{"main.go", "func (a *app) handleWallet(", "wallet_get:"},
		{"main.go", "func (a *app) handleTransferAddressState(", "address_get:"},
		{"main.go", "func (a *app) handleIndex(", "index_html:"},
		{"main.go", "func (a *app) handleExplorer(", "explorer_html:"},
		{"status_api.go", "func (a *app) handleStatus(", "status_get:"},
		{"sup_api.go", "func (a *app) handleSUPEarnings(", "sup_earnings:"},
	}
	for _, tc := range cases {
		src, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		i := strings.Index(string(src), tc.fn)
		if i < 0 {
			t.Fatalf("%s missing %s", tc.file, tc.fn)
		}
		body := string(src)[i:]
		if j := strings.Index(body, "\nfunc "); j > 0 {
			body = body[:j]
		}
		if !strings.Contains(body, "allowRate") || !strings.Contains(body, tc.key) {
			t.Fatalf("%s: expected allowRate(%q…)", tc.fn, tc.key)
		}
	}
}
