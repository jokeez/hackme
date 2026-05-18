package sandbox

import (
	"context"
	"testing"
)

func TestEvalLockWasm(t *testing.T) {
	ctx := context.Background()
	v, err := Eval(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	// 10*7+13 = 83
	if v != 83 {
		t.Fatalf("eval(10)=%d want 83", v)
	}
}
