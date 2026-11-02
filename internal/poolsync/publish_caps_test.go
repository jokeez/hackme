package poolsync

import "testing"

func TestOrdersPublishMaxBudgetRunsCoversHuntHeavy(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", "")
	got := OrdersPublishMaxBudgetRuns()
	if got < 12000 {
		t.Fatalf("default max budget_runs=%d must cover hunt_heavy 12000", got)
	}
	if got != DefaultOrdersPublishMaxBudgetRuns {
		t.Fatalf("got %d want default %d", got, DefaultOrdersPublishMaxBudgetRuns)
	}
}

func TestOrdersPublishMaxBudgetRunsOverride(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", "2048")
	if OrdersPublishMaxBudgetRuns() != 2048 {
		t.Fatalf("override ignored")
	}
}
