package chain

import (
	"context"
	"testing"
	"time"
)

func TestMinerSubscribeLogLinesReceivesAppendLine(t *testing.T) {
	m := NewMiner(0.01, nil, nil, InternalTaskProvider{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.SubscribeLogLines(ctx)
	m.appendLine("test-line-alpha")
	select {
	case got := <-ch:
		if got != "test-line-alpha" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for log line")
	}
}

func TestMinerSubscribeLogLinesClosesWhenCancelled(t *testing.T) {
	m := NewMiner(0.01, nil, nil, InternalTaskProvider{})
	ctx, cancel := context.WithCancel(context.Background())
	ch := m.SubscribeLogLines(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for close")
	}
}
