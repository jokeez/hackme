package workerfuzzloop

import (
	"context"
	"os"
	"testing"
)

func TestResearchSlotDefaultOff(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "")
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")
	cfg := ResearchSlotFromEnv()
	if cfg.Enabled {
		t.Fatal("research slot must default OFF")
	}
	res := MaybeRunResearchSlot(context.Background(), cfg)
	if res.Ran || res.SkippedReason != "disabled" {
		t.Fatalf("res=%+v", res)
	}
}

func TestResearchSlotStubWhenEnabled(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	cfg := ResearchSlotFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	res := MaybeRunResearchSlot(context.Background(), cfg)
	if res.Ran || res.SkippedReason != "stub_not_wired" {
		t.Fatalf("stub must not run LF yet, got %+v", res)
	}
}

func TestPoolSeedFromResearchUntouchedByResearchSlot(t *testing.T) {
	t.Setenv("HACKME_WORKER_RESEARCH_SLOT", "1")
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")
	if Truthy(os.Getenv("HACKME_POOL_SEED_FROM_RESEARCH")) {
		t.Fatal("SEED_FROM_RESEARCH must stay default OFF")
	}
}
