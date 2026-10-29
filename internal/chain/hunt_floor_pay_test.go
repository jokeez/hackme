package chain

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPayFuzzRunFloorIsFractional(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE_FLOOR_PAY_BPS", "1000") // 10%
	dir := t.TempDir()
	svc, err := Open(filepath.Join(dir, "floor.db"), DefaultGenesis())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	ctx := context.Background()
	camp := "hunt-floor-frac"
	miner := "HMC-1234567890123456"
	if _, err := svc.OpenFuzzEscrowSplit(ctx, camp, 20, 8, "50_50"); err != nil {
		t.Fatal(err)
	}
	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, "evt-floor-1", "run_floor", camp, miner, "")
	if err != nil {
		t.Fatal(err)
	}
	if !newly || row == nil {
		t.Fatalf("want newly applied floor, got newly=%v row=%v", newly, row)
	}
	full, err := svc.GetFuzzEscrow(ctx, camp)
	if err != nil || full == nil {
		t.Fatal(err)
	}
	// 10% of per_run; runs_done still advances by 1
	if full.RunsDone != 1 {
		t.Fatalf("runs_done=%d want 1", full.RunsDone)
	}
	if full.RunsPaidUnits == 0 || full.RunsPaidUnits >= full.PerRunUnits {
		t.Fatalf("floor paid %d; want 0 < paid < per_run %d", full.RunsPaidUnits, full.PerRunUnits)
	}
	want := full.PerRunUnits / 10
	if full.RunsPaidUnits != want && full.RunsPaidUnits != want+1 {
		// allow off-by-one from integer division min-1 path only when per_run tiny
		if full.PerRunUnits >= 10 && full.RunsPaidUnits != want {
			t.Fatalf("paid %d want ~%d (10%% of %d)", full.RunsPaidUnits, want, full.PerRunUnits)
		}
	}
}
