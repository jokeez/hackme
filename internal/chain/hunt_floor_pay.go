package chain

import (
	"os"
	"strconv"
	"strings"
)

// DefaultHuntEngineFloorPayBPS is 10% of per_run_units for CLEAN coverage
// shards without proven edges (docs/HUNT_ECONOMICS.md: floor ≠ full PayRun).
const DefaultHuntEngineFloorPayBPS = 1000

// HuntEngineFloorPayBPS returns the fractional run-pool debit for Hunt floor
// settles. Override: HACKME_HUNT_ENGINE_FLOOR_PAY_BPS (1..10000).
func HuntEngineFloorPayBPS() int {
	v := strings.TrimSpace(os.Getenv("HACKME_HUNT_ENGINE_FLOOR_PAY_BPS"))
	if v == "" {
		return DefaultHuntEngineFloorPayBPS
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return DefaultHuntEngineFloorPayBPS
	}
	if n > 10000 {
		return 10000
	}
	return n
}
