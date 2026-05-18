package chain

import "testing"

func TestMinerSetSoftCPUThrottlePct(t *testing.T) {
	m := NewMiner(0.01, nil, nil, InternalTaskProvider{})
	if err := m.SetSoftCPUThrottlePct(75); err != nil {
		t.Fatal(err)
	}
	if got := m.softThrottlePct(); got != 75 {
		t.Fatalf("got %v want 75", got)
	}
	if err := m.SetSoftCPUThrottlePct(0); err == nil {
		t.Fatal("expected error")
	}
	if err := m.SetSoftCPUThrottlePct(101); err == nil {
		t.Fatal("expected error")
	}
}
