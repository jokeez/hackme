package poolfuzz

import (
	"strings"

	"hackme/internal/fuzzupstream"
)

// catalogSharedTarget is the catalog id whose on-disk seed cache is shared
// across campaigns. A second catalog order on mpack should start from units
// the first order already proved, not from "{}".
//
// Inventory and customer-harness orders are not catalog. Their inputs stay
// inside that campaign: writing them into the shared cache would hand one
// customer's bytes to the next order on the same target id.
func catalogSharedTarget(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	source := strings.ToLower(strings.TrimSpace(jsonString(cfg["hunt_source"])))
	if source != "catalog" {
		return ""
	}
	id := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	clean, ok := fuzzupstream.SanitizeCatalogID(id)
	if !ok {
		return ""
	}
	return clean
}
