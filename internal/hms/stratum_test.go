package hms

import "testing"

func TestParseStratumSubmitNonce(t *testing.T) {
	n, err := parseStratumSubmitNonce([]any{"w", "12345"})
	if err != nil || n != 12345 {
		t.Fatalf("simple: %d %v", n, err)
	}
	n, err = parseStratumSubmitNonce([]any{"w", "job", "ex", "nt", "999"})
	if err != nil || n != 999 {
		t.Fatalf("antminer: %d %v", n, err)
	}
	n, err = parseStratumSubmitNonce([]any{"w", "job", "ex", "nt", "0x2a"})
	if err != nil || n != 42 {
		t.Fatalf("hex: %d %v", n, err)
	}
}
