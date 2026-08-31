package hunt

import (
	"path/filepath"
	"sort"
	"strings"

	"os"

	"hackme/internal/fuzzingcli"
)

// PackSuggestion maps inventory/repo signals to Dig guard packs or Hunt paths.
type PackSuggestion struct {
	PackID      string  `json:"pack_id"`
	Title       string  `json:"title"`
	Score       int     `json:"score"`
	Reason      string  `json:"reason"`
	Product     string  `json:"product"` // dig | hunt
	WizardHint  string  `json:"wizard_hint,omitempty"`
	DefaultPkg  string  `json:"default_package,omitempty"`
}

type packRule struct {
	id      string
	product string
	score   int
	reason  string
	match   func(path, content string) bool
}

var packRules = []packRule{
	{id: "secrets", product: "dig", score: 90, reason: "secret/token/env patterns in path or source",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "secret") || strings.Contains(h, "token") || strings.Contains(h, "credential") ||
				strings.Contains(h, "akia") || strings.Contains(h, "api_key") || strings.Contains(h, ".env")
		}},
	{id: "filter_utf8", product: "dig", score: 85, reason: "filter/display/utf-8 parser surface",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "filter") || strings.Contains(h, "utf8") || strings.Contains(h, "unicode") ||
				strings.Contains(h, "display") || strings.Contains(h, "flux")
		}},
	{id: "parser_expat", product: "dig", score: 88, reason: "XML/expat/libxml parser surface",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "xml") || strings.Contains(h, "expat") || strings.Contains(h, "libxml") ||
				strings.HasSuffix(p, ".xml") || strings.Contains(c, "<?xml")
		}},
	{id: "script_bounds", product: "dig", score: 80, reason: "script/consensus push bounds class",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "script") || strings.Contains(h, "opcode") || strings.Contains(h, "consensus") ||
				strings.Contains(h, "pushdata")
		}},
	{id: "bounds_smoke", product: "dig", score: 55, reason: "numeric bounds / range guard smoke",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "bound") || strings.Contains(h, "range") || strings.Contains(h, "clamp")
		}},
	{id: "overflow_smoke", product: "dig", score: 52, reason: "overflow/wrap arithmetic guard patterns",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "overflow") || strings.Contains(h, "wrapping_mul") || strings.Contains(h, "saturating")
		}},
	{id: "state_smoke", product: "dig", score: 50, reason: "state machine / transition guards",
		match: func(p, c string) bool {
			h := strings.ToLower(p + "\n" + c)
			return strings.Contains(h, "state") || strings.Contains(h, "transition") || strings.Contains(h, "fsm")
		}},
	{id: "hunt_reuse", product: "hunt", score: 95, reason: "LLVMFuzzerTestOneInput present — Hunt reuse ready",
		match: func(p, c string) bool {
			return strings.Contains(c, inventoryMarker)
		}},
}

// SuggestPacksForPath scores Dig/Hunt suggestions for one relative file path and optional content sample.
func SuggestPacksForPath(sourceRel, contentSample string) []PackSuggestion {
	sourceRel = strings.TrimSpace(sourceRel)
	contentSample = strings.TrimSpace(contentSample)
	if len(contentSample) > 4096 {
		contentSample = contentSample[:4096]
	}
	base := strings.ToLower(filepath.Base(sourceRel))
	scored := make([]PackSuggestion, 0, 4)
	seen := map[string]struct{}{}
	for _, rule := range packRules {
		if !rule.match(sourceRel+" "+base, contentSample) {
			continue
		}
		if _, ok := seen[rule.id]; ok {
			continue
		}
		seen[rule.id] = struct{}{}
		sug := PackSuggestion{
			PackID:  rule.id,
			Score:   rule.score,
			Reason:  rule.reason,
			Product: rule.product,
		}
		if rule.product == "hunt" {
			sug.Title = "Hunt reuse (inventory target)"
			sug.WizardHint = "hunt create --source " + sourceRel
		} else if p, err := fuzzingcli.GuardPackFor(rule.id); err == nil {
			sug.Title = p.Title
			sug.DefaultPkg = p.DefaultPackage
			sug.WizardHint = "hackme-fuzzing wizard --pack " + p.ID + " --package " + p.DefaultPackage
		} else {
			sug.Title = rule.id
		}
		scored = append(scored, sug)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].PackID < scored[j].PackID
	})
	if len(scored) > 6 {
		scored = scored[:6]
	}
	return scored
}

// SuggestPacksForInventory aggregates pack-map hints across inventory targets.
func SuggestPacksForInventory(res *InventoryResult) []PackSuggestion {
	if res == nil || len(res.Targets) == 0 {
		return nil
	}
	agg := map[string]*PackSuggestion{}
	for _, t := range res.Targets {
		sample := inventoryContentSample(res.Path, t.Path)
		for _, s := range SuggestPacksForPath(t.Path, sample) {
			cur, ok := agg[s.PackID]
			if !ok || s.Score > cur.Score {
				cp := s
				agg[s.PackID] = &cp
			}
		}
	}
	out := make([]PackSuggestion, 0, len(agg))
	for _, s := range agg {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].PackID < out[j].PackID
	})
	return out
}

func inventoryContentSample(root, rel string) string {
	root = strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if root == "" || rel == "" {
		return inventoryMarker
	}
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return inventoryMarker
	}
	if len(b) > 4096 {
		b = b[:4096]
	}
	return string(b)
}
