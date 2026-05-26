package main

import (
	"net/http"
)

// writeWalletResponse hides operator treasury from the public internet and integrators.
// Loopback desktop dashboard and valid admin token see address, balance, and data paths.
func (a *app) writeWalletResponse(w http.ResponseWriter, r *http.Request, full map[string]any) {
	expected := adminTokenFromEnv()
	if expected != "" && secretsEqualConstantTime(extractAdminSecret(r), expected) {
		writeJSON(w, full)
		return
	}
	// Same-origin desktop UI on 127.0.0.1 needs spendable balance; remote clients stay redacted.
	if envBool("HACKME_DESKTOP_MODE", false) && requestFromLoopback(r) {
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
