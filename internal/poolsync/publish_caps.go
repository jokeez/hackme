package poolsync

import (
	"os"
	"strconv"
	"strings"
)

// Orders publish token caps — must stay aligned with cmd/coordinator/orders_publish.go.
// Defaults allow hunt_heavy (12000 budget_runs) with headroom.
const (
	DefaultOrdersPublishMaxBudgetRuns    = 16384
	DefaultOrdersPublishMaxBudgetSeconds = 86400
)

// OrdersPublishMaxBudgetRuns is the publish-token ceiling for campaign budget_runs.
func OrdersPublishMaxBudgetRuns() int {
	return envIntDefault("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", DefaultOrdersPublishMaxBudgetRuns)
}

// OrdersPublishMaxBudgetSeconds is the publish-token ceiling for budget_seconds.
func OrdersPublishMaxBudgetSeconds() int {
	return envIntDefault("HACKME_ORDERS_PUBLISH_MAX_BUDGET_SECONDS", DefaultOrdersPublishMaxBudgetSeconds)
}

func envIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}
