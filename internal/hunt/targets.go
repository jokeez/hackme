package hunt

import (
	"sort"

	"hackme/internal/fuzzupstream"
)

const catalogDisclaimer = "Catalog targets ship pinned OSS profiles — not your private repo until Hunt inventory + Accept (Phase 2)."

// ListCatalogTargets returns reuse-ready targets from upstream/oss_cve_targets.json.
func ListCatalogTargets(repoRoot string, priorityMax int) ([]TargetSummary, error) {
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]TargetSummary, 0, len(manifest.Targets))
	for _, t := range manifest.Targets {
		if priorityMax > 0 && t.Priority > priorityMax {
			continue
		}
		lang := fuzzupstream.TargetLanguage(t)
		out = append(out, TargetSummary{
			ID:         t.ID,
			Title:      t.Title,
			Repo:       t.Repo,
			Ref:        t.Ref,
			Source:     "catalog",
			Language:   lang,
			Driver:     t.Driver,
			WasmGuard:  t.WasmGuard,
			CWE:        append([]string(nil), t.CWE...),
			Priority:   t.Priority,
			ReuseReady: t.Driver != "",
			Disclosure: catalogDisclaimer,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
