package main

import (
	"net/http"
)

// writeWalletResponse hides operator treasury from the public internet and integrators.
// Only loopback operators with admin token see address, balance, and data paths.
func (a *app) writeWalletResponse(w http.ResponseWriter, r *http.Request, full map[string]any) {
	// Unlike other admin routes, never expose treasury when HACKME_ADMIN_TOKEN is unset.
	expected := adminTokenFromEnv()
	if expected != "" && secretsEqualConstantTime(extractAdminSecret(r), expected) {
		writeJSON(w, full)
		return
	}
	writeJSON(w, map[string]any{
		"public_redacted": true,
		"billing_model":   "network_operator_escrow",
		"do_not_send_hmc": true,
		"note": "Order prepaid escrow is debited from the canonical node operator wallet — not your personal address. " +
			"Do not send HMC to any third-party address claiming to be HackMe billing. " +
			"Track your work with order id and payer_ref. POST /api/tasks returns HTTP 402 if operator escrow is insufficient.",
		"integrator_docs": "https://hackme.tech/developers.html",
	})
}
