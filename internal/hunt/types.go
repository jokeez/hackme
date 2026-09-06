// Package hunt implements Hunt MVP (repo inventory, packages, local ASAN runs).
package hunt

// PackageInfo is a customer-facing Hunt SKU preset.
type PackageInfo struct {
	Key                string  `json:"key"`
	Title              string  `json:"title"`
	BudgetHMC          float64 `json:"budget_hmc"`
	BudgetShards       int     `json:"budget_shards"`
	IterationsPerShard int     `json:"iterations_per_shard"`
	LocalBudgetIters   int     `json:"local_budget_iterations"`
	LocalTimeLimitSec  int     `json:"local_time_limit_sec"`
	MinPerShard        float64 `json:"min_per_shard_hmc"`
	WallHours          string  `json:"wall_hours"`
	Summary            string  `json:"summary"`
	EscrowSplit        string  `json:"escrow_split"`
}

// TargetSummary is a catalog or inventory fuzz target entry.
type TargetSummary struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Repo       string   `json:"repo,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Source     string   `json:"source"` // catalog | inventory
	Driver     string   `json:"driver,omitempty"`
	Path       string   `json:"path,omitempty"`
	Language   string   `json:"language,omitempty"` // c | cpp | rust
	WasmGuard  string   `json:"wasm_guard,omitempty"`
	CWE        []string `json:"cwe,omitempty"`
	Priority   int      `json:"priority,omitempty"`
	ReuseReady bool     `json:"reuse_ready"`
	Disclosure string   `json:"disclaimer,omitempty"`
}

// InventoryRequest scans a local path for LLVMFuzzerTestOneInput harnesses.
type InventoryRequest struct {
	Path     string `json:"path"`
	MaxFiles int    `json:"max_files,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

// InventoryResult is the response from POST /api/hunt/inventory.
type InventoryResult struct {
	Path         string          `json:"path"`
	ScannedFiles int             `json:"scanned_files"`
	Targets      []TargetSummary `json:"targets"`
	BuildHints   []string        `json:"build_hints,omitempty"`
	Disclaimer   string          `json:"disclaimer"`
}

// CreateRequest starts a Hunt campaign on the local node.
type CreateRequest struct {
	ID              string          `json:"id,omitempty"`
	Title           string          `json:"title"`
	Package         string          `json:"package"` // hunt_lite | hunt_standard
	TargetID        string          `json:"target_id,omitempty"`
	Catalog         bool            `json:"catalog,omitempty"`
	Inventory       *TargetSummary  `json:"inventory_target,omitempty"`
	Repo            *RepoPinRequest `json:"repo,omitempty"`
	TemplateAccept  bool            `json:"template_accept,omitempty"`
	BudgetHMC       float64         `json:"budget_hmc,omitempty"`
	BudgetShards    int             `json:"budget_shards,omitempty"`
	Status          string          `json:"status,omitempty"`
	PoolDistributed bool            `json:"pool_distributed,omitempty"`
}

// HarnessBuildAPIRequest is POST /api/hunt/harness/build body.
type HarnessBuildAPIRequest struct {
	Repo           *RepoPinRequest `json:"repo,omitempty"`
	SourceRel      string          `json:"source_rel"`
	TemplateAccept bool            `json:"template_accept,omitempty"`
}
