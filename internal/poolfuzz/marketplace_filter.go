package poolfuzz

import (
	"strings"
)

// IsInternalGateCampaign marks health-check / probe campaigns that must not appear
// in the public miner marketplace or workerfuzz claim UI.
func IsInternalGateCampaign(id, title, ownerRef string, cfg map[string]any) bool {
	id = strings.TrimSpace(strings.ToLower(id))
	title = strings.TrimSpace(strings.ToLower(title))
	ownerRef = strings.TrimSpace(strings.ToLower(ownerRef))
	if id == "" && title == "" && ownerRef == "" {
		return false
	}
	if v, ok := cfg["internal_gate"]; ok {
		switch x := v.(type) {
		case bool:
			if x {
				return true
			}
		case string:
			if strings.EqualFold(strings.TrimSpace(x), "true") || strings.TrimSpace(x) == "1" {
				return true
			}
		case float64:
			if x != 0 {
				return true
			}
		}
	}
	if strings.HasPrefix(id, "pool-sync-gate") {
		return true
	}
	if strings.Contains(id, "pool-sync-gate") {
		return true
	}
	if strings.HasPrefix(id, "pool-sync-node-") {
		return true
	}
	if strings.HasSuffix(id, "-probe") || strings.Contains(id, "-probe-") {
		return true
	}
	if strings.HasPrefix(id, "campaign-gate-") {
		return true
	}
	if strings.HasPrefix(id, "probe-") || strings.HasPrefix(id, "probe") {
		return true
	}
	if strings.HasPrefix(id, "l1v4-") {
		return true
	}
	if strings.HasPrefix(id, "campaign-customer-up-") {
		return true
	}
	if strings.HasPrefix(id, "test-") || strings.HasPrefix(id, "verify-") {
		return true
	}
	if strings.Contains(title, "penny") || strings.Contains(title, "smoke") {
		return true
	}
	if title == "pool-sync-gate" || title == "probe" || title == "gate-audit" {
		return true
	}
	if strings.Contains(title, "pool sync") && strings.Contains(title, "gate") {
		return true
	}
	if strings.HasPrefix(ownerRef, "gate:") {
		return true
	}
	if strings.HasPrefix(ownerRef, "tier-gate:") || strings.HasPrefix(ownerRef, "tier:") {
		return true
	}
	if strings.HasPrefix(title, "tier-gate") || strings.HasPrefix(title, "tier-debug") || strings.HasPrefix(title, "tier-audit") {
		return true
	}
	if pr := strings.TrimSpace(strings.ToLower(jsonString(cfg["payer_ref"]))); strings.HasPrefix(pr, "gate:") {
		return true
	}
	if pr := strings.TrimSpace(strings.ToLower(jsonString(cfg["payer_ref"]))); strings.HasPrefix(pr, "tier-gate:") || strings.HasPrefix(pr, "tier:") {
		return true
	}
	return false
}

// IsMarketplaceCampaign is true for pool-distributed campaigns miners should see.
func IsMarketplaceCampaign(status, id, title, ownerRef string, cfg map[string]any) bool {
	st := strings.TrimSpace(strings.ToLower(status))
	if st != "planned" && st != "running" {
		return false
	}
	if IsInternalGateCampaign(id, title, ownerRef, cfg) {
		return false
	}
	return true
}
