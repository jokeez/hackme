package main

import (
	"testing"
	"time"

	"hackme/internal/lanpool"
)

func TestRigRegistryUpsertAndOnline(t *testing.T) {
	reg := lanpool.NewRegistry()
	if err := reg.Upsert("192.168.1.50:9999", lanpool.PushWorkBody{
		WorkerID:    "rig-a",
		Name:        "Test rig",
		HashrateGHS: 42.5,
	}); err != nil {
		t.Fatal(err)
	}
	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("len %d", len(list))
	}
	if list[0].WorkerID != "rig-a" || list[0].HashrateGHS != 42.5 || !list[0].Online {
		t.Fatalf("%+v", list[0])
	}
	reg.SetRigLastSeen("rig-a", time.Now().Add(-2*time.Minute))
	list2 := reg.List()
	if list2[0].Online {
		t.Fatal("expected offline after stale LastSeen")
	}
}
