package fuzznative

import "testing"

func TestEvalReproEvalScriptPush(t *testing.T) {
	// truncated OP_PUSHDATA2 → bad opcode
	input := []byte{0x41, 0, 0, 0, 0, 0, 0, 0} // 65-byte push, truncated script
	res := EvalRepro("bitcoin", "bitcoin_evalscript_push", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s", res.Status)
	}
}

func TestEvalReproWitnessStack(t *testing.T) {
	// lane 0 elem size > 520
	input := []byte{0x09, 0x03, 0, 0, 0, 0, 0, 0} // 0x0309 = 777
	res := EvalRepro("bitcoin", "bitcoin_witness_stack", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s", res.Status)
	}
}

func TestEvalReproEvalScriptStack(t *testing.T) {
	var u uint64 = 800 | (250 << 12) // main+alt > 1000
	input := make([]byte, 8)
	for i := 0; i < 8; i++ {
		input[i] = byte(u >> (8 * i))
	}
	res := EvalRepro("bitcoin", "bitcoin_evalscript_stack", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s", res.Status)
	}
}

func TestEvalReproOpCount(t *testing.T) {
	input := make([]byte, 8)
	input[0] = 250 // repeat NOP
	res := EvalRepro("bitcoin", "bitcoin_evalscript_opcount", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s", res.Status)
	}
}

func TestEvalReproTxCheckMoneyRange(t *testing.T) {
	input := []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}
	res := EvalRepro("bitcoin", "bitcoin_tx_check", input, nil)
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s", res.Status)
	}
}
