//go:build huntlsanprobe

package hunt_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"hackme/internal/hunt"
)

func TestLocalHuntLibuclSanitizerHygiene(t *testing.T) {
	if os.Getenv("HACKME_HUNT_LSAN_PROBE") != "1" {
		t.Skip("set HACKME_HUNT_LSAN_PROBE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rep, err := hunt.LocalRun(ctx, hunt.LocalRunOptions{
		RepoRoot:         hunt.RepoRoot(),
		TargetID:         "libucl",
		BudgetIterations: 5000,
		TimeLimitSec:     60,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("libucl: verdict=%s iter=%d crashes=%d", rep.Verdict, rep.Iterations, len(rep.Crashes))
	bySub := map[string]int{}
	for _, c := range rep.Crashes {
		key := c.SanitizerClass + "/" + c.SanitizerSubtype
		bySub[key]++
	}
	t.Logf("subtypes=%v", bySub)
	if len(rep.Crashes) == 0 {
		t.Fatal("expected ubsan hygiene signals on libucl")
	}
	if rep.Verdict != "INFORMATIONAL" {
		t.Fatalf("verdict=%s want INFORMATIONAL", rep.Verdict)
	}
	foundUBSan := false
	for k := range bySub {
		if len(k) >= 5 && k[:5] == "ubsan" {
			foundUBSan = true
		}
	}
	if !foundUBSan {
		t.Fatalf("no ubsan subtypes in %v", bySub)
	}
	fmt.Println("SUMMARY ok libucl informational ubsan subtypes=", len(bySub))
}
