package main

import (
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/zeebo/blake3"

	"hackme/internal/netutil"
)

func publicWorkerPseudoKey(workerID string) string {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return ""
	}
	sum := blake3.Sum256([]byte("hackme-public-worker-v1|" + workerID))
	return hex.EncodeToString(sum[:8])
}

func stripDesktopWorkerFields(ws map[string]any) {
	if ws == nil {
		return
	}
	delete(ws, "desktop_worker_id")
	delete(ws, "desktop_worker_injected")
	delete(ws, "desktop_worker_on_coordinator")
}

// redactPublicActiveRigRows pseudonymizes active_rigs worker_id/name (same key as workers map);
// raw ids there would let anyone recompute and de-anonymize the workers pseudonyms.
func redactPublicActiveRigRows(ws map[string]any) {
	rows, ok := ws["active_rigs"].([]any)
	if !ok || len(rows) == 0 {
		return
	}
	red := make([]any, 0, len(rows))
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["worker_id"].(string)
		key := publicWorkerPseudoKey(id)
		if key == "" {
			continue
		}
		cp := make(map[string]any, len(row))
		for k, v := range row {
			switch k {
			case "ip", "client_ip", "last_client_ip":
				continue
			}
			cp[k] = v
		}
		cp["worker_id"] = key
		cp["name"] = key
		red = append(red, cp)
	}
	ws["active_rigs"] = red
}

// publicWorkStatsSensitiveKeys are operator / lease / PoH-modulus internals that
// must not ship on unauthenticated /api/work/stats. Honest miners receive
// share_mod + nonce range on the authenticated claim path.
var publicWorkStatsSensitiveKeys = []string{
	"source", // set separately to a public-safe label after redaction
	"target_mod",
	"target_mod_updated_unix",
	"target_mod_min",
	"target_mod_max",
	"target_mod_load_hint",
	"target_mod_load_mode",
	"target_mod_load_capped",
	"share_divisor",
	"default_batch",
	"next_nonce",
	"issued_ranges",
	"reissued_ranges",
	"drop_reason_count",
	"claim_per_min",
	"submit_per_min",
	"ban_sec",
	"bad_strikes_to_ban",
	"max_claim_batch",
	"max_active_leases",
	"max_active_leases_per_worker",
	"max_dedup_entries",
	"max_workers",
	"attempt_recheck_budget",
	"attempt_recheck_pct",
	"attempt_recheck_rejects",
	"attempt_recheck_rolls",
	"last_signed_miner_address",
	"lease_sec",
	"lease_work_sec",
	"active_leases_count",
	"expired_leases",
	"ingress_q_len",
	"scheduler_transitions",
	"reward_per_m",
	"share_price_ema_per_hash_hmc",
	"share_price_instant_units",
	"share_price_samples",
	"share_rate_low_flags",
	"share_rate_low_rejects",
	"share_rate_min",
	"share_rate_min_expected",
	"share_rate_reject",
}

// publicCoordinatorSourceLabel never returns a loopback/private coordinator URL
// on unauthenticated edges (recon). Public clients use the TLS pool proxy.
func publicCoordinatorSourceLabel(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "pool"
	}
	if netutil.IsLoopbackURL(base) {
		return "pool"
	}
	if u, err := url.Parse(base); err == nil {
		host := strings.ToLower(strings.TrimSpace(u.Hostname()))
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			return "pool"
		}
	}
	// Still avoid advertising raw internal upstreams; public path is the pool proxy.
	return "pool"
}

// redactInternalCoordinatorURLs replaces loopback coordinator URLs in status maps
// so public HTTPS edges do not advertise 127.0.0.1:18083.
func redactInternalCoordinatorURLs(m map[string]any) {
	if m == nil {
		return
	}
	for _, k := range []string{
		"pool_coordinator_url",
		"pool_coordinator_url_effective",
		"source",
	} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" && netutil.IsLoopbackURL(v) {
			m[k] = publicCoordinatorSourceLabel(v)
		}
	}
	if ux, ok := m["miner_public_ux"].(map[string]any); ok {
		if v, ok := ux["coordinator_url_effective"].(string); ok && netutil.IsLoopbackURL(v) {
			ux["coordinator_url_effective"] = defaultPublicStagingCoordinatorBase
			ux["coordinator_url_redacted"] = true
		}
	}
}

// redactPublicWorkStats strips desktop id leakage, re-keys workers, and removes
// lease/PoH-modulus internals for unauthenticated callers.
func redactPublicWorkStats(ws map[string]any) {
	if ws == nil {
		return
	}
	stripDesktopWorkerFields(ws)
	redactPublicActiveRigRows(ws)
	for _, k := range publicWorkStatsSensitiveKeys {
		delete(ws, k)
	}
	// Keep transparency totals; drop nested anti-abuse / emit knobs that are not
	// needed for public pool boards.
	if pb, ok := ws["pool_budget"].(map[string]any); ok && pb != nil {
		ws["pool_budget"] = map[string]any{
			"enabled":           pb["enabled"],
			"daily_budget_hmc":  pb["daily_budget_hmc"],
			"accrued_today_hmc": pb["accrued_today_hmc"],
			"persisted":         pb["persisted"],
			"note":              pb["note"],
		}
	}
	if sp, ok := ws["sup_policy"].(map[string]any); ok && sp != nil {
		ws["sup_policy"] = map[string]any{
			"enabled":          sp["enabled"],
			"on_chain_settle":  sp["on_chain_settle"],
			"require_hybrid":   sp["require_hybrid"],
			"listing_note":     sp["listing_note"],
			"economics_v2":     sp["economics_v2"],
			"total_payout_sup": sp["total_payout_sup"],
		}
	}
	raw, ok := ws["workers"].(map[string]any)
	if !ok || len(raw) == 0 {
		ws["workers_keys"] = "pseudonym_v1"
		return
	}
	out := make(map[string]any, len(raw))
	for id, row := range raw {
		key := publicWorkerPseudoKey(id)
		if key == "" {
			continue
		}
		m, ok := row.(map[string]any)
		if !ok {
			out[key] = row
			continue
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			switch k {
			case "last_client_ip", "client_ip", "ip":
				continue
			default:
				cp[k] = v
			}
		}
		if prev, ok := out[key].(map[string]any); ok {
			for k, v := range cp {
				prev[k] = v
			}
			out[key] = prev
			continue
		}
		out[key] = cp
	}
	ws["workers"] = out
	ws["workers_count"] = uint64(len(out))
	ws["workers_keys"] = "pseudonym_v1"
}
